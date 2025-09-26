package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"log-streamer/internal/model"
)

var (
	targetURL   = flag.String("target", "http://localhost:8080/ingest", "Target distributor URL")
	rate        = flag.Int("rate", 1000, "Packets per second")
	concurrency = flag.Int("concurrency", 10, "Number of concurrent goroutines")
	duration    = flag.Duration("duration", 0, "Duration to run (0 = infinite)")
	packetSize  = flag.Int("packet-size", 5, "Number of messages per packet")
	emitterID   = flag.String("emitter-id", "emitter-1", "Emitter ID")
)

func main() {
	flag.Parse()

	log.Printf("Starting emitter with configuration:")
	log.Printf("  Target: %s", *targetURL)
	log.Printf("  Rate: %d packets/sec", *rate)
	log.Printf("  Concurrency: %d", *concurrency)
	if *duration > 0 {
		log.Printf("  Duration: %v", *duration)
	} else {
		log.Printf("  Duration: infinite")
	}
	log.Printf("  Packet size: %d messages", *packetSize)
	log.Printf("  Emitter ID: %s", *emitterID)

	// Create HTTP client with improved connection management
	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        1000, // Increased from 100
			MaxIdleConnsPerHost: 100,  // Increased from 10
			MaxConnsPerHost:     200,  // Added connection limit per host
			IdleConnTimeout:     90 * time.Second,
			DisableKeepAlives:   false, // Ensure keep-alives are enabled
			DisableCompression:  true,  // Disable compression for better performance
		},
	}

	// Create emitter
	emitter := NewEmitter(client, *targetURL, *emitterID, *packetSize)

	// Start emitter
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		log.Printf("Received signal: %v, shutting down...", sig)
		cancel()
	}()

	// Run emitter
	emitter.Run(ctx, *rate, *concurrency, *duration)
}

// Emitter generates and sends log packets
type Emitter struct {
	client     *http.Client
	targetURL  string
	emitterID  string
	packetSize int
	packetID   int64
	mu         sync.Mutex
}

// NewEmitter creates a new emitter
func NewEmitter(client *http.Client, targetURL, emitterID string, packetSize int) *Emitter {
	return &Emitter{
		client:     client,
		targetURL:  targetURL,
		emitterID:  emitterID,
		packetSize: packetSize,
	}
}

// Run starts the emitter
func (e *Emitter) Run(ctx context.Context, rate, concurrency int, duration time.Duration) {
	// Calculate interval between packets
	interval := time.Second / time.Duration(rate)

	// Create channels for coordination
	packetChan := make(chan *model.LogPacket, concurrency*2)
	done := make(chan struct{})

	// Start workers
	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			e.worker(ctx, packetChan, workerID)
		}(i)
	}

	// Start packet generator
	go func() {
		defer close(packetChan)
		e.generatePackets(ctx, packetChan, interval, duration)
	}()

	// Wait for completion
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Println("Emitter completed")
	case <-ctx.Done():
		log.Println("Emitter cancelled")
	}
}

// generatePackets generates packets at the specified rate
func (e *Emitter) generatePackets(ctx context.Context, packetChan chan<- *model.LogPacket, interval time.Duration, duration time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var endTime time.Time
	if duration > 0 {
		endTime = time.Now().Add(duration)
	}

	for {
		select {
		case <-ticker.C:
			// Check if we should stop
			if !endTime.IsZero() && time.Now().After(endTime) {
				return
			}

			// Generate packet
			packet := e.generatePacket()

			// Send to workers
			select {
			case packetChan <- packet:
			case <-ctx.Done():
				return
			default:
				log.Printf("Warning: packet channel full, dropping packet %s", packet.PacketID)
			}

		case <-ctx.Done():
			return
		}
	}
}

// worker processes packets from the channel
func (e *Emitter) worker(ctx context.Context, packetChan <-chan *model.LogPacket, workerID int) {
	var successCount, errorCount int64

	for {
		select {
		case packet, ok := <-packetChan:
			if !ok {
				log.Printf("Worker %d: channel closed, processed %d packets (%d success, %d errors)",
					workerID, successCount+errorCount, successCount, errorCount)
				return
			}

			if e.sendPacket(packet) {
				atomic.AddInt64(&successCount, 1)
			} else {
				atomic.AddInt64(&errorCount, 1)
			}

		case <-ctx.Done():
			log.Printf("Worker %d: cancelled, processed %d packets (%d success, %d errors)",
				workerID, successCount+errorCount, successCount, errorCount)
			return
		}
	}
}

// generatePacket generates a random log packet
func (e *Emitter) generatePacket() *model.LogPacket {
	e.mu.Lock()
	e.packetID++
	packetID := e.packetID
	e.mu.Unlock()

	packet := &model.LogPacket{
		EmitterID: e.emitterID,
		PacketID:  fmt.Sprintf("%s-%d", e.emitterID, packetID),
		Messages:  make([]model.LogMessage, e.packetSize),
	}

	// Generate random messages
	levels := []string{"INFO", "WARN", "ERROR", "DEBUG"}
	topics := []string{"auth", "database", "api", "cache", "queue"}

	for i := 0; i < e.packetSize; i++ {
		packet.Messages[i] = model.LogMessage{
			Timestamp: time.Now().UnixMilli(),
			Level:     levels[rand.Intn(len(levels))],
			Message:   fmt.Sprintf("Sample log message %d from %s", i+1, topics[rand.Intn(len(topics))]),
			Metadata: map[string]string{
				"service":  "demo-service",
				"version":  "1.0.0",
				"instance": fmt.Sprintf("instance-%d", rand.Intn(10)),
			},
		}
	}

	return packet
}

// sendPacket sends a packet to the distributor
func (e *Emitter) sendPacket(packet *model.LogPacket) bool {
	// Convert packet to JSON
	jsonData, err := json.Marshal(packet)
	if err != nil {
		log.Printf("Failed to marshal packet %s: %v", packet.PacketID, err)
		return false
	}

	// Create HTTP request
	req, err := http.NewRequest("POST", e.targetURL, bytes.NewBuffer(jsonData))
	if err != nil {
		log.Printf("Failed to create request for packet %s: %v", packet.PacketID, err)
		return false
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "log-streamer-emitter/1.0")
	req.Header.Set("Connection", "keep-alive") // Explicitly set keep-alive

	// Send request with retry logic
	maxRetries := 3
	for attempt := 0; attempt < maxRetries; attempt++ {
		resp, err := e.client.Do(req)
		if err != nil {
			if attempt < maxRetries-1 {
				// Wait before retry
				time.Sleep(time.Duration(attempt+1) * 100 * time.Millisecond)
				continue
			}
			log.Printf("Failed to send packet %s (attempt %d/%d): %v", packet.PacketID, attempt+1, maxRetries, err)
			return false
		}

		// Read and close response body to free connection
		_, readErr := io.Copy(io.Discard, resp.Body)
		resp.Body.Close()

		if readErr != nil {
			log.Printf("Failed to read response body for packet %s: %v", packet.PacketID, readErr)
		}

		// Check response
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return true
		}

		log.Printf("Packet %s returned status %d (attempt %d/%d)", packet.PacketID, resp.StatusCode, attempt+1, maxRetries)
		if attempt < maxRetries-1 {
			time.Sleep(time.Duration(attempt+1) * 100 * time.Millisecond)
		}
	}

	return false
}

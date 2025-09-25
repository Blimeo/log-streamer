package worker

import (
	"bytes"
	"context"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"log-streamer/internal/model"
)

// Sender handles sending packets to a specific analyzer
type Sender struct {
	analyzer     *model.AnalyzerConfig
	queue        <-chan *model.LogPacket
	httpClient   *http.Client
	config       *SenderConfig
	router       RouterInterface
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	failureCount int
	healthy      bool
	mu           sync.RWMutex
}

// SenderConfig holds configuration for the sender
type SenderConfig struct {
	Retries     int
	Timeout     time.Duration
	MaxFailures int
}

// RouterInterface defines the interface for router operations
type RouterInterface interface {
	SetAnalyzerHealth(id string, healthy bool)
}

// NewSender creates a new sender for an analyzer
func NewSender(analyzer *model.AnalyzerConfig, queue <-chan *model.LogPacket, config *SenderConfig, router RouterInterface) *Sender {
	ctx, cancel := context.WithCancel(context.Background())

	httpClient := &http.Client{
		Timeout: config.Timeout,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
		},
	}

	return &Sender{
		analyzer:   analyzer,
		queue:      queue,
		httpClient: httpClient,
		config:     config,
		router:     router,
		ctx:        ctx,
		cancel:     cancel,
	}
}

// Start starts the sender goroutine
func (s *Sender) Start() {
	s.wg.Add(1)
	go s.run()
}

// Stop stops the sender
func (s *Sender) Stop() {
	s.cancel()
	s.wg.Wait()
}

// run is the main sender loop
func (s *Sender) run() {
	defer s.wg.Done()

	failureCount := 0

	for {
		select {
		case packet, ok := <-s.queue:
			if !ok {
				log.Printf("Sender for %s: queue closed, stopping", s.analyzer.ID)
				return
			}

			success := s.sendPacket(packet)

			if success {
				failureCount = 0
				s.mu.Lock()
				s.failureCount = 0
				s.healthy = true
				s.mu.Unlock()
			} else {
				failureCount++

				s.mu.Lock()
				s.failureCount = failureCount
				s.mu.Unlock()

				// Mark analyzer as unhealthy if failure threshold exceeded
				if failureCount >= s.config.MaxFailures {
					s.router.SetAnalyzerHealth(s.analyzer.ID, false)
					s.mu.Lock()
					s.healthy = false
					s.mu.Unlock()
					log.Printf("Analyzer %s marked as unhealthy after %d consecutive failures", s.analyzer.ID, failureCount)
				}
			}

		case <-s.ctx.Done():
			log.Printf("Sender for %s: context cancelled, stopping", s.analyzer.ID)
			return
		}
	}
}

// sendPacket sends a single packet to the analyzer
func (s *Sender) sendPacket(packet *model.LogPacket) bool {
	// Convert packet to JSON
	jsonData, err := packet.ToJSON()
	if err != nil {
		log.Printf("Failed to marshal packet for %s: %v", s.analyzer.ID, err)
		return false
	}

	// Try to send with retries
	for attempt := 0; attempt <= s.config.Retries; attempt++ {
		if attempt > 0 {
			// Exponential backoff
			backoff := time.Duration(attempt) * 100 * time.Millisecond
			select {
			case <-time.After(backoff):
			case <-s.ctx.Done():
				return false
			}
		}

		success := s.sendHTTPRequest(jsonData)
		if success {
			if attempt > 0 {
				log.Printf("Packet sent to %s successfully on attempt %d", s.analyzer.ID, attempt+1)
			}
			return true
		}

		if attempt < s.config.Retries {
			log.Printf("Failed to send packet to %s (attempt %d/%d), retrying...", s.analyzer.ID, attempt+1, s.config.Retries+1)
		}
	}

	log.Printf("Failed to send packet to %s after %d attempts", s.analyzer.ID, s.config.Retries+1)
	return false
}

// sendHTTPRequest sends an HTTP request to the analyzer
func (s *Sender) sendHTTPRequest(jsonData []byte) bool {
	req, err := http.NewRequestWithContext(s.ctx, "POST", s.analyzer.URL+"/ingest", bytes.NewBuffer(jsonData))
	if err != nil {
		log.Printf("Failed to create request for %s: %v", s.analyzer.ID, err)
		return false
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "log-streamer-distributor/1.0")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		log.Printf("HTTP request failed for %s: %v", s.analyzer.ID, err)
		return false
	}
	defer resp.Body.Close()

	// Read response body to avoid connection leaks
	_, err = io.Copy(io.Discard, resp.Body)
	if err != nil {
		log.Printf("Failed to read response body from %s: %v", s.analyzer.ID, err)
	}

	// Check if response is successful
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return true
	}

	log.Printf("Analyzer %s returned status %d", s.analyzer.ID, resp.StatusCode)
	return false
}

// HealthCheck performs a health check on the analyzer
func (s *Sender) HealthCheck() bool {
	req, err := http.NewRequestWithContext(s.ctx, "GET", s.analyzer.URL+"/health", nil)
	if err != nil {
		return false
	}

	req.Header.Set("User-Agent", "log-streamer-distributor/1.0")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	// Read response body
	_, err = io.Copy(io.Discard, resp.Body)
	if err != nil {
		log.Printf("Failed to read health check response from %s: %v", s.analyzer.ID, err)
	}

	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

// GetID returns the analyzer ID
func (s *Sender) GetID() string {
	return s.analyzer.ID
}

// GetURL returns the analyzer URL
func (s *Sender) GetURL() string {
	return s.analyzer.URL
}

// SetHealthy sets the health status
func (s *Sender) SetHealthy(healthy bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.healthy = healthy
}

// IsHealthy returns the health status
func (s *Sender) IsHealthy() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.healthy
}

// GetFailureCount returns the failure count
func (s *Sender) GetFailureCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.failureCount
}

// UpdateRouterHealth updates the router's health status for this analyzer
func (s *Sender) UpdateRouterHealth(healthy bool) {
	s.router.SetAnalyzerHealth(s.analyzer.ID, healthy)
}

package router

import (
	"context"
	"fmt"
	"log"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"log-streamer/internal/metrics"
	"log-streamer/internal/model"
)

// Represents a single analyzer with its state
type Analyzer struct {
	Config       model.AnalyzerConfig
	Queue        chan *model.LogPacket
	Health       *model.HealthStatus
	PacketCount  int64
	MessagesSent uint64 // Atomic counter for messages sent to this analyzer
	mu           sync.RWMutex
}

// Handles packet routing to analyzers using message-based weighted selection
type Router struct {
	analyzers   []*Analyzer
	mu          sync.RWMutex
	ctx         context.Context
	cancel      context.CancelFunc
	requestChan chan *model.RouteRequest
}

// Creates a new router with the given analyzers
func NewRouter(analyzers []model.AnalyzerConfig, queueSize int) *Router {
	ctx, cancel := context.WithCancel(context.Background())

	router := &Router{
		analyzers:   make([]*Analyzer, len(analyzers)),
		ctx:         ctx,
		cancel:      cancel,
		requestChan: make(chan *model.RouteRequest, 1000), // Buffer for request channel
	}

	// Initialize analyzers
	for i, config := range analyzers {
		router.analyzers[i] = &Analyzer{
			Config:       config,
			Queue:        make(chan *model.LogPacket, queueSize),
			MessagesSent: 0,
			Health: &model.HealthStatus{
				Healthy:      true,
				LastCheck:    time.Now(),
				FailureCount: 0,
			},
		}
	}

	return router
}

// Routes a packet synchronously and sends response via reply channel
func (r *Router) RouteRequest(req *model.RouteRequest) {
	response := r.routePacket(req.Packet)
	select {
	case req.Reply <- response:
	case <-r.ctx.Done():
		// If context is cancelled, send error response
		req.Reply <- model.RouteResponse{
			Success: false,
			Error:   fmt.Errorf("router context cancelled"),
		}
	}
}

// routePacket performs the actual routing logic and returns a response
func (r *Router) routePacket(packet *model.LogPacket) model.RouteResponse {
	healthyAnalyzers := r.getHealthyAnalyzers()
	if len(healthyAnalyzers) == 0 {
		return model.RouteResponse{
			Success: false,
			Error:   fmt.Errorf("no healthy analyzers available"),
		}
	}

	// Use message-based weighted selection to choose analyzer
	analyzer := r.selectAnalyzerForPacket(healthyAnalyzers, uint64(len(packet.Messages)))
	if analyzer == nil {
		return model.RouteResponse{
			Success: false,
			Error:   fmt.Errorf("no healthy analyzers available"),
		}
	}

	// Send packet to analyzer's queue
	select {
	case analyzer.Queue <- packet:
		// Only increment counters on successful enqueue
		atomic.AddInt64(&analyzer.PacketCount, 1)
		atomic.AddUint64(&analyzer.MessagesSent, uint64(len(packet.Messages)))

		// Update Prometheus metrics
		metrics.TotalPackets.Inc()
		metrics.TotalMessages.Add(float64(len(packet.Messages)))
		metrics.PacketsByAnalyzer.WithLabelValues(analyzer.Config.ID).Inc()
		metrics.MessagesByAnalyzer.WithLabelValues(analyzer.Config.ID).Add(float64(len(packet.Messages)))

		return model.RouteResponse{
			Success: true,
			Error:   nil,
		}
	case <-r.ctx.Done():
		log.Printf("Failed to enqueue packet %s to analyzer %s: router context cancelled",
			packet.PacketID, analyzer.Config.ID)
		return model.RouteResponse{
			Success: false,
			Error:   fmt.Errorf("router context cancelled"),
		}
	default:
		log.Printf("Failed to enqueue packet %s to analyzer %s: queue is full",
			packet.PacketID, analyzer.Config.ID)
		return model.RouteResponse{
			Success: false,
			Error:   fmt.Errorf("analyzer %s queue is full", analyzer.Config.ID),
		}
	}
}

// getHealthyAnalyzers returns a list of currently healthy analyzers
func (r *Router) getHealthyAnalyzers() []*Analyzer {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var healthy []*Analyzer
	for _, analyzer := range r.analyzers {
		analyzer.mu.RLock()
		if analyzer.Health.Healthy {
			healthy = append(healthy, analyzer)
		}
		analyzer.mu.RUnlock()
	}

	return healthy
}

// selectAnalyzerForPacket selects an analyzer using message-based weighted selection
// It chooses the analyzer that minimizes (messages_sent + m) / weight
func (r *Router) selectAnalyzerForPacket(analyzers []*Analyzer, messageCount uint64) *Analyzer {
	if len(analyzers) == 0 {
		return nil
	}
	if len(analyzers) == 1 {
		return analyzers[0]
	}

	var chosen *Analyzer
	var bestMetric float64 = math.Inf(1)

	for _, analyzer := range analyzers {
		// Read messages sent atomically
		messagesSent := atomic.LoadUint64(&analyzer.MessagesSent)

		// Calculate metric: (messages_sent + m) / weight
		metric := (float64(messagesSent) + float64(messageCount)) / analyzer.Config.Weight

		if metric < bestMetric {
			bestMetric = metric
			chosen = analyzer
		} else if metric == bestMetric {
			// Tie-break by lowest messages_sent to ensure fairness
			if messagesSent < atomic.LoadUint64(&chosen.MessagesSent) {
				chosen = analyzer
			}
		}
	}

	return chosen
}

// Returns an analyzer by ID
func (r *Router) GetAnalyzer(id string) *Analyzer {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, analyzer := range r.analyzers {
		if analyzer.Config.ID == id {
			return analyzer
		}
	}
	return nil
}

// Returns all analyzers
func (r *Router) GetAllAnalyzers() []*Analyzer {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Return a copy to avoid race conditions
	analyzers := make([]*Analyzer, len(r.analyzers))
	copy(analyzers, r.analyzers)
	return analyzers
}

// Updates the health status of an analyzer
func (r *Router) SetAnalyzerHealth(id string, healthy bool) {
	analyzer := r.GetAnalyzer(id)
	if analyzer == nil {
		log.Printf("Warning: analyzer %s not found", id)
		return
	}

	analyzer.mu.Lock()
	defer analyzer.mu.Unlock()

	wasHealthy := analyzer.Health.Healthy
	analyzer.Health.Healthy = healthy
	analyzer.Health.LastCheck = time.Now()

	if !healthy {
		analyzer.Health.FailureCount++
	} else {
		analyzer.Health.FailureCount = 0
	}

	if wasHealthy != healthy {
		status := "unhealthy"
		if healthy {
			status = "healthy"
		}
		log.Printf("Analyzer %s is now %s (failure count: %d)", id, status, analyzer.Health.FailureCount)
	}
}

// Returns current routing metrics
func (r *Router) GetMetrics() model.Metrics {
	analyzers := r.GetAllAnalyzers()

	metrics := model.Metrics{
		PacketsByAnalyzer:    make(map[string]int64),
		MessagesByAnalyzer:   make(map[string]uint64),
		QueueDepthByAnalyzer: make(map[string]int),
		Timestamp:            time.Now(),
	}

	healthyCount := 0
	for _, analyzer := range analyzers {
		packetCount := atomic.LoadInt64(&analyzer.PacketCount)
		messageCount := atomic.LoadUint64(&analyzer.MessagesSent)
		queueDepth := len(analyzer.Queue)

		metrics.PacketsByAnalyzer[analyzer.Config.ID] = packetCount
		metrics.MessagesByAnalyzer[analyzer.Config.ID] = messageCount
		metrics.QueueDepthByAnalyzer[analyzer.Config.ID] = queueDepth
		metrics.TotalPackets += packetCount
		metrics.TotalMessages += messageCount

		analyzer.mu.RLock()
		if analyzer.Health.Healthy {
			healthyCount++
		}
		analyzer.mu.RUnlock()
	}

	metrics.HealthyAnalyzers = healthyCount
	metrics.TotalAnalyzers = len(analyzers)

	return metrics
}

// Updates Prometheus gauge metrics
func (r *Router) UpdatePrometheusGauges() {
	analyzers := r.GetAllAnalyzers()

	healthyCount := 0
	for _, analyzer := range analyzers {
		queueDepth := len(analyzer.Queue)
		metrics.QueueDepth.WithLabelValues(analyzer.Config.ID).Set(float64(queueDepth))

		analyzer.mu.RLock()
		if analyzer.Health.Healthy {
			healthyCount++
		}
		analyzer.mu.RUnlock()
	}

	metrics.HealthyAnalyzers.Set(float64(healthyCount))
	metrics.TotalAnalyzers.Set(float64(len(analyzers)))
}

// Starts the request handler goroutine
func (r *Router) StartRequestHandler() {
	go func() {
		for {
			select {
			case req := <-r.requestChan:
				r.RouteRequest(req)
			case <-r.ctx.Done():
				// Close any pending requests with error response
				for {
					select {
					case req := <-r.requestChan:
						req.Reply <- model.RouteResponse{
							Success: false,
							Error:   fmt.Errorf("router stopped"),
						}
					default:
						return
					}
				}
			}
		}
	}()
}

// Submits a routing request and returns the response channel
func (r *Router) SubmitRequest(packet *model.LogPacket) chan model.RouteResponse {
	reply := make(chan model.RouteResponse, 1)
	req := &model.RouteRequest{
		Packet: packet,
		Reply:  reply,
	}

	select {
	case r.requestChan <- req:
		return reply
	case <-r.ctx.Done():
		reply <- model.RouteResponse{
			Success: false,
			Error:   fmt.Errorf("router stopped"),
		}
		return reply
	default:
		reply <- model.RouteResponse{
			Success: false,
			Error:   fmt.Errorf("router request channel full"),
		}
		return reply
	}
}

// Stops the router
func (r *Router) Stop() {
	r.cancel()

	// Close all analyzer queues
	for _, analyzer := range r.analyzers {
		close(analyzer.Queue)
	}
}

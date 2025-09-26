package health

import (
	"context"
	"log"
	"sync"
	"time"
)

// Performs health checks on analyzers
type Checker struct {
	analyzers []AnalyzerInterface
	interval  time.Duration
	timeout   time.Duration
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
}

// Defines the interface for analyzer health operations
type AnalyzerInterface interface {
	GetID() string
	GetURL() string
	HealthCheck() bool
	SetHealthy(healthy bool)
	IsHealthy() bool
	GetFailureCount() int
	UpdateRouterHealth(healthy bool)
}

// Creates a new health checker
func NewChecker(analyzers []AnalyzerInterface, interval, timeout time.Duration) *Checker {
	ctx, cancel := context.WithCancel(context.Background())

	return &Checker{
		analyzers: analyzers,
		interval:  interval,
		timeout:   timeout,
		ctx:       ctx,
		cancel:    cancel,
	}
}

// Starts the health checker
func (c *Checker) Start() {
	c.wg.Add(1)
	go c.run()
}

// Stops the health checker
func (c *Checker) Stop() {
	c.cancel()
	c.wg.Wait()
}

// run is the main health check loop
func (c *Checker) run() {
	defer c.wg.Done()

	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	// Perform initial health check
	c.performHealthChecks()

	for {
		select {
		case <-ticker.C:
			c.performHealthChecks()
		case <-c.ctx.Done():
			log.Println("Health checker stopped")
			return
		}
	}
}

// performHealthChecks performs health checks on all analyzers
func (c *Checker) performHealthChecks() {
	var wg sync.WaitGroup

	for _, analyzer := range c.analyzers {
		wg.Add(1)
		go func(a AnalyzerInterface) {
			defer wg.Done()
			c.checkAnalyzer(a)
		}(analyzer)
	}

	wg.Wait()
}

// checkAnalyzer performs a health check on a single analyzer
func (c *Checker) checkAnalyzer(analyzer AnalyzerInterface) {
	// Create a context with timeout for this health check
	ctx, cancel := context.WithTimeout(c.ctx, c.timeout)
	defer cancel()

	// Perform health check in a goroutine to respect timeout
	done := make(chan bool, 1)
	go func() {
		done <- analyzer.HealthCheck()
	}()

	var healthy bool
	select {
	case healthy = <-done:
		// Health check completed
	case <-ctx.Done():
		// Health check timed out
		healthy = false
		log.Printf("Health check for %s timed out after %v", analyzer.GetID(), c.timeout)
	}

	// Update analyzer health status
	wasHealthy := analyzer.IsHealthy()
	analyzer.SetHealthy(healthy)

	// Update router health status
	analyzer.UpdateRouterHealth(healthy)

	// Log status changes
	if wasHealthy != healthy {
		status := "unhealthy"
		if healthy {
			status = "healthy"
		}
		log.Printf("Analyzer %s is now %s (failure count: %d)", analyzer.GetID(), status, analyzer.GetFailureCount())
	}
}

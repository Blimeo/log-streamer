package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"log-streamer/internal/api"
	"log-streamer/internal/config"
	"log-streamer/internal/health"
	"log-streamer/internal/metrics"
	"log-streamer/internal/router"
	"log-streamer/internal/worker"

	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	// Load configuration
	cfg, err := config.LoadFromEnv()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	if err := cfg.Validate(); err != nil {
		log.Fatalf("Invalid configuration: %v", err)
	}

	log.Printf("Starting distributor with configuration:")
	log.Printf("  Port: %s", cfg.Port)
	log.Printf("  Ingest buffer size: %d", cfg.IngestBufferSize)
	log.Printf("  Per-analyzer queue size: %d", cfg.PerAnalyzerQueueSize)
	log.Printf("  Analyzers: %d", len(cfg.Analyzers))
	for _, analyzer := range cfg.Analyzers {
		log.Printf("    - %s: %s (weight: %.2f)", analyzer.ID, analyzer.URL, analyzer.Weight)
	}

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create router
	router := router.NewRouter(cfg.Analyzers, cfg.PerAnalyzerQueueSize)

	// Start router request handler
	router.StartRequestHandler()

	// Register Prometheus metrics
	metrics.Register()
	log.Printf("Prometheus metrics registered")

	// Create API handler
	apiHandler := api.NewDistributorAPI(router)

	// Create HTTP server
	server := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: setupRoutes(apiHandler),
	}

	// Create metrics server
	metricsAddr := os.Getenv("METRICS_ADDR")
	if metricsAddr == "" {
		metricsAddr = ":9100"
	}
	metricsServer := &http.Server{
		Addr:    metricsAddr,
		Handler: setupMetricsRoutes(),
	}

	// Start analyzer senders
	senders := startAnalyzers(ctx, router, cfg)

	// Start health checker
	healthChecker := startHealthChecker(ctx, senders, cfg)

	// Start periodic Prometheus gauge updates
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				router.UpdatePrometheusGauges()
			case <-ctx.Done():
				return
			}
		}
	}()

	// Start HTTP server
	go func() {
		log.Printf("Starting HTTP server on port %s", cfg.Port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server failed: %v", err)
		}
	}()

	// Start metrics server
	go func() {
		log.Printf("Starting metrics server on %s", metricsAddr)
		if err := metricsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Metrics server failed: %v", err)
		}
	}()

	// Wait for shutdown signal
	waitForShutdown()

	log.Println("Shutting down distributor...")

	// Cancel context to stop all goroutines
	cancel()

	// Shutdown HTTP servers
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP server shutdown error: %v", err)
	}

	if err := metricsServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("Metrics server shutdown error: %v", err)
	}

	// Stop all components
	router.Stop()
	healthChecker.Stop()

	// Stop all senders
	for _, sender := range senders {
		sender.Stop()
	}

	log.Println("Distributor stopped")
}

// setupRoutes sets up HTTP routes
func setupRoutes(apiHandler *api.DistributorAPI) http.Handler {
	r := mux.NewRouter()

	// API routes
	r.HandleFunc("/ingest", apiHandler.IngestHandler).Methods("POST")
	r.HandleFunc("/metrics-json", apiHandler.MetricsHandler).Methods("GET")
	r.HandleFunc("/health", apiHandler.HealthHandler).Methods("GET")

	// Add middleware
	r.Use(loggingMiddleware)
	r.Use(corsMiddleware)

	return r
}

// setupMetricsRoutes sets up routes for the metrics server
func setupMetricsRoutes() http.Handler {
	r := mux.NewRouter()

	// Prometheus metrics endpoint
	r.Handle("/metrics", promhttp.Handler())

	// Add middleware
	r.Use(loggingMiddleware)
	r.Use(corsMiddleware)

	return r
}

// startAnalyzers starts sender goroutines for all analyzers
func startAnalyzers(ctx context.Context, router *router.Router, cfg *config.Config) []*worker.Sender {
	var senders []*worker.Sender

	analyzers := router.GetAllAnalyzers()
	for _, analyzer := range analyzers {
		senderConfig := &worker.SenderConfig{
			Retries:     cfg.SenderRetries,
			Timeout:     cfg.SenderTimeout,
			MaxFailures: cfg.MaxFailures,
		}

		sender := worker.NewSender(&analyzer.Config, analyzer.Queue, senderConfig, router)
		sender.Start()
		senders = append(senders, sender)

		log.Printf("Started sender for analyzer %s", analyzer.Config.ID)
	}

	return senders
}

// startHealthChecker starts the health checker
func startHealthChecker(ctx context.Context, senders []*worker.Sender, cfg *config.Config) *health.Checker {
	// Convert senders to analyzer interfaces
	var analyzers []health.AnalyzerInterface
	for _, sender := range senders {
		analyzers = append(analyzers, sender)
	}

	healthChecker := health.NewChecker(analyzers, cfg.HealthCheckInterval, cfg.HealthCheckTimeout)
	healthChecker.Start()

	log.Printf("Started health checker with interval %v", cfg.HealthCheckInterval)
	return healthChecker
}

// waitForShutdown waits for shutdown signals
func waitForShutdown() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigChan
	log.Printf("Received signal: %v", sig)
}

// loggingMiddleware logs HTTP requests
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
	})
}

// corsMiddleware adds CORS headers
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

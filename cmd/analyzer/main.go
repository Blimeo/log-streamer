package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"log-streamer/internal/model"

	"github.com/gorilla/mux"
)

func main() {
	// Get configuration from environment
	port := os.Getenv("PORT")
	if port == "" {
		port = "9001"
	}

	analyzerID := os.Getenv("ANALYZER_ID")
	if analyzerID == "" {
		analyzerID = "analyzer-1"
	}

	// Optional: simulate processing delay
	processingDelay := 0
	if delayStr := os.Getenv("PROCESSING_DELAY_MS"); delayStr != "" {
		if delay, err := strconv.Atoi(delayStr); err == nil {
			processingDelay = delay
		}
	}

	log.Printf("Starting analyzer %s on port %s", analyzerID, port)
	if processingDelay > 0 {
		log.Printf("Processing delay: %dms", processingDelay)
	}

	// Create HTTP server
	server := &http.Server{
		Addr:    ":" + port,
		Handler: setupRoutes(analyzerID, processingDelay),
	}

	log.Printf("Analyzer %s listening on port %s", analyzerID, port)
	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

// setupRoutes sets up HTTP routes
func setupRoutes(analyzerID string, processingDelay int) http.Handler {
	r := mux.NewRouter()

	// Ingest endpoint
	r.HandleFunc("/ingest", func(w http.ResponseWriter, r *http.Request) {
		handleIngest(w, r, analyzerID, processingDelay)
	}).Methods("POST")

	// Health endpoint
	r.HandleFunc("/health", handleHealth).Methods("GET")

	// Metrics endpoint
	r.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		handleMetrics(w, r, analyzerID)
	}).Methods("GET")

	// Add middleware
	r.Use(loggingMiddleware)
	r.Use(corsMiddleware)

	return r
}

// handleIngest processes incoming log packets
func handleIngest(w http.ResponseWriter, r *http.Request, analyzerID string, processingDelay int) {
	// Parse the request body
	var packet model.LogPacket
	if err := json.NewDecoder(r.Body).Decode(&packet); err != nil {
		log.Printf("Failed to decode packet: %v", err)
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Simulate processing delay
	if processingDelay > 0 {
		time.Sleep(time.Duration(processingDelay) * time.Millisecond)
	}

	// Process the packet (in a real analyzer, this would do actual analysis)
	processPacket(&packet, analyzerID)

	// Return success response
	response := map[string]string{
		"status":    "processed",
		"packet_id": packet.PacketID,
		"analyzer":  analyzerID,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// processPacket processes a log packet
func processPacket(packet *model.LogPacket, analyzerID string) {

	// In a real analyzer, this would perform actual log analysis
	// For demo purposes, we just log some statistics
	levelCounts := make(map[string]int)
	for _, msg := range packet.Messages {
		levelCounts[msg.Level]++
	}

}

// handleHealth handles health check requests
func handleHealth(w http.ResponseWriter, r *http.Request) {
	response := map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleMetrics handles metrics requests
func handleMetrics(w http.ResponseWriter, r *http.Request, analyzerID string) {
	// In a real analyzer, this would return actual metrics
	response := map[string]interface{}{
		"analyzer_id": analyzerID,
		"status":      "running",
		"timestamp":   time.Now(),
		"uptime":      "demo",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
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

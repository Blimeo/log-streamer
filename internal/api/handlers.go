package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"log-streamer/internal/model"
)

// Handles HTTP requests for the distributor
type DistributorAPI struct {
	router  RouterInterface
	metrics MetricsInterface
}

// Defines the interface for router operations
type RouterInterface interface {
	SubmitRequest(packet *model.LogPacket) chan model.RouteResponse
	GetMetrics() model.Metrics
}

// Defines the interface for metrics operations
type MetricsInterface interface {
	GetMetrics() model.Metrics
}

// Creates a new API handler
func NewDistributorAPI(router RouterInterface) *DistributorAPI {
	return &DistributorAPI{
		router:  router,
		metrics: router,
	}
}

// Handles POST /ingest requests
func (api *DistributorAPI) IngestHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse the request body
	var packet model.LogPacket
	if err := json.NewDecoder(r.Body).Decode(&packet); err != nil {
		log.Printf("Failed to decode request body: %v", err)
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Validate the packet
	if err := validatePacket(&packet); err != nil {
		log.Printf("Invalid packet: %v", err)
		http.Error(w, fmt.Sprintf("Invalid packet: %v", err), http.StatusBadRequest)
		return
	}

	// Submit routing request and wait for response with timeout
	responseChan := api.router.SubmitRequest(&packet)

	select {
	case routeResponse := <-responseChan:
		if routeResponse.Success {
			// Successfully routed
			w.WriteHeader(http.StatusAccepted)
			response := map[string]string{
				"status":    "accepted",
				"packet_id": packet.PacketID,
			}
			json.NewEncoder(w).Encode(response)
		} else {
			// Routing failed, return appropriate error
			log.Printf("Failed to route packet %s: %v", packet.PacketID, routeResponse.Error)
			http.Error(w, "Service temporarily unavailable", http.StatusServiceUnavailable)
		}
	case <-time.After(250 * time.Millisecond):
		// Timeout waiting for routing response
		log.Printf("Timeout waiting for routing response for packet %s", packet.PacketID)
		http.Error(w, "Service temporarily unavailable", http.StatusServiceUnavailable)
	}
}

// Handles GET /metrics requests
func (api *DistributorAPI) MetricsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	metrics := api.metrics.GetMetrics()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(metrics)
}

// Handles GET /health requests
func (api *DistributorAPI) HealthHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	metrics := api.metrics.GetMetrics()

	// Consider the service healthy if at least one analyzer is healthy
	healthy := metrics.HealthyAnalyzers > 0

	status := "healthy"
	statusCode := http.StatusOK
	if !healthy {
		status = "unhealthy"
		statusCode = http.StatusServiceUnavailable
	}

	response := map[string]interface{}{
		"status":            status,
		"healthy_analyzers": metrics.HealthyAnalyzers,
		"total_analyzers":   metrics.TotalAnalyzers,
		"timestamp":         time.Now(),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(response)
}

// validatePacket validates a log packet
func validatePacket(packet *model.LogPacket) error {
	if packet.PacketID == "" {
		return fmt.Errorf("packet_id is required")
	}

	if packet.EmitterID == "" {
		return fmt.Errorf("emitter_id is required")
	}

	if len(packet.Messages) == 0 {
		return fmt.Errorf("at least one message is required")
	}

	for i, msg := range packet.Messages {
		if msg.Message == "" {
			return fmt.Errorf("message %d: message content is required", i)
		}

		if msg.Level == "" {
			return fmt.Errorf("message %d: level is required", i)
		}

		if msg.Timestamp <= 0 {
			return fmt.Errorf("message %d: timestamp must be positive", i)
		}
	}

	return nil
}

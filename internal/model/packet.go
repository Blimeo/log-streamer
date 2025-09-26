package model

import (
	"encoding/json"
	"time"
)

// Represents a single log entry
type LogMessage struct {
	Timestamp int64             `json:"timestamp"`
	Level     string            `json:"level"`
	Message   string            `json:"message"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// Represents a collection of log messages from an emitter
type LogPacket struct {
	EmitterID string       `json:"emitter_id"`
	Messages  []LogMessage `json:"messages"`
	PacketID  string       `json:"packet_id"`
}

// Represents configuration for an analyzer
type AnalyzerConfig struct {
	ID     string  `json:"id"`
	URL    string  `json:"url"`
	Weight float64 `json:"weight"`
}

// Represents the health status of an analyzer
type HealthStatus struct {
	Healthy      bool      `json:"healthy"`
	LastCheck    time.Time `json:"last_check"`
	FailureCount int       `json:"failure_count"`
}

// Represents distribution metrics
type Metrics struct {
	TotalPackets         int64             `json:"total_packets"`
	TotalMessages        uint64            `json:"total_messages"`
	PacketsByAnalyzer    map[string]int64  `json:"packets_by_analyzer"`
	MessagesByAnalyzer   map[string]uint64 `json:"messages_by_analyzer"`
	QueueDepthByAnalyzer map[string]int    `json:"queue_depth_by_analyzer"`
	HealthyAnalyzers     int               `json:"healthy_analyzers"`
	TotalAnalyzers       int               `json:"total_analyzers"`
	Timestamp            time.Time         `json:"timestamp"`
}

// Converts a LogPacket to JSON bytes
func (p *LogPacket) ToJSON() ([]byte, error) {
	return json.Marshal(p)
}

// Represents a routing request with reply channel
type RouteRequest struct {
	Packet *LogPacket
	Reply  chan RouteResponse
}

// Represents the response from routing
type RouteResponse struct {
	Success bool
	Error   error
}

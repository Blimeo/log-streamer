package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"log-streamer/internal/model"
)

// Holds all configuration for the distributor
type Config struct {
	// Server configuration
	Port             string `json:"port"`
	IngestBufferSize int    `json:"ingest_buffer_size"`

	// Analyzer configuration
	Analyzers            []model.AnalyzerConfig `json:"analyzers"`
	PerAnalyzerQueueSize int                    `json:"per_analyzer_queue_size"`

	// Sender configuration
	SenderRetries int           `json:"sender_retries"`
	SenderTimeout time.Duration `json:"sender_timeout"`

	// Health check configuration
	HealthCheckInterval time.Duration `json:"health_check_interval"`
	HealthCheckTimeout  time.Duration `json:"health_check_timeout"`

	// Circuit breaker configuration
	MaxFailures int `json:"max_failures"`
}

// Returns a default configuration
func DefaultConfig() *Config {
	return &Config{
		Port:                 "8080",
		IngestBufferSize:     10000,
		PerAnalyzerQueueSize: 1000,
		SenderRetries:        3,
		SenderTimeout:        500 * time.Millisecond,
		HealthCheckInterval:  5 * time.Second,
		HealthCheckTimeout:   2 * time.Second,
		MaxFailures:          5,
	}
}

// Loads configuration from environment variables
func LoadFromEnv() (*Config, error) {
	config := DefaultConfig()

	// Load port
	if port := os.Getenv("PORT"); port != "" {
		config.Port = port
	}

	// Load buffer sizes
	if bufferSize := os.Getenv("INGEST_BUFFER_SIZE"); bufferSize != "" {
		if size, err := strconv.Atoi(bufferSize); err == nil {
			config.IngestBufferSize = size
		}
	}

	if queueSize := os.Getenv("PER_ANALYZER_QUEUE_SIZE"); queueSize != "" {
		if size, err := strconv.Atoi(queueSize); err == nil {
			config.PerAnalyzerQueueSize = size
		}
	}

	// Load sender configuration
	if retries := os.Getenv("SENDER_RETRIES"); retries != "" {
		if r, err := strconv.Atoi(retries); err == nil {
			config.SenderRetries = r
		}
	}

	if timeout := os.Getenv("SENDER_TIMEOUT_MS"); timeout != "" {
		if ms, err := strconv.Atoi(timeout); err == nil {
			config.SenderTimeout = time.Duration(ms) * time.Millisecond
		}
	}

	// Load health check configuration
	if interval := os.Getenv("HEALTH_CHECK_INTERVAL_SEC"); interval != "" {
		if sec, err := strconv.Atoi(interval); err == nil {
			config.HealthCheckInterval = time.Duration(sec) * time.Second
		}
	}

	if timeout := os.Getenv("HEALTH_CHECK_TIMEOUT_SEC"); timeout != "" {
		if sec, err := strconv.Atoi(timeout); err == nil {
			config.HealthCheckTimeout = time.Duration(sec) * time.Second
		}
	}

	// Load analyzers from environment
	analyzers, err := parseAnalyzersFromEnv()
	if err != nil {
		return nil, fmt.Errorf("failed to parse analyzers: %w", err)
	}
	config.Analyzers = analyzers

	return config, nil
}

// parseAnalyzersFromEnv parses analyzer configuration from ANALYZERS environment variable
// Format: "http://analyzer-1:9001=0.4,http://analyzer-2:9002=0.3,http://analyzer-3:9003=0.3"
func parseAnalyzersFromEnv() ([]model.AnalyzerConfig, error) {
	analyzersStr := os.Getenv("ANALYZERS")
	if analyzersStr == "" {
		// Default analyzers for demo
		return []model.AnalyzerConfig{
			{ID: "analyzer-1", URL: "http://analyzer-1:9001", Weight: 0.4},
			{ID: "analyzer-2", URL: "http://analyzer-2:9002", Weight: 0.3},
			{ID: "analyzer-3", URL: "http://analyzer-3:9003", Weight: 0.3},
		}, nil
	}

	var analyzers []model.AnalyzerConfig
	pairs := strings.Split(analyzersStr, ",")

	for i, pair := range pairs {
		parts := strings.Split(pair, "=")
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid analyzer format: %s", pair)
		}

		url := strings.TrimSpace(parts[0])
		weightStr := strings.TrimSpace(parts[1])

		weight, err := strconv.ParseFloat(weightStr, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid weight for analyzer %s: %w", url, err)
		}

		// Extract ID from URL (e.g., "analyzer-1" from "http://analyzer-1:9001")
		id := fmt.Sprintf("analyzer-%d", i+1)
		if strings.Contains(url, "analyzer-") {
			parts := strings.Split(url, "analyzer-")
			if len(parts) > 1 {
				portParts := strings.Split(parts[1], ":")
				if len(portParts) > 0 {
					id = "analyzer-" + portParts[0]
				}
			}
		}

		analyzers = append(analyzers, model.AnalyzerConfig{
			ID:     id,
			URL:    url,
			Weight: weight,
		})
	}

	return analyzers, nil
}

// Validates the configuration
func (c *Config) Validate() error {
	if c.Port == "" {
		return fmt.Errorf("port is required")
	}

	if len(c.Analyzers) == 0 {
		return fmt.Errorf("at least one analyzer is required")
	}

	// Validate weights sum to approximately 1.0
	var totalWeight float64
	for _, analyzer := range c.Analyzers {
		if analyzer.Weight <= 0 {
			return fmt.Errorf("analyzer %s has invalid weight: %f", analyzer.ID, analyzer.Weight)
		}
		totalWeight += analyzer.Weight
	}

	if totalWeight < 0.9 || totalWeight > 1.1 {
		return fmt.Errorf("analyzer weights should sum to approximately 1.0, got: %f", totalWeight)
	}

	return nil
}

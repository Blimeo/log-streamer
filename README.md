# Log Streamer Distributor

A high-performance log distribution system built in Go that routes log packets to multiple analyzers using weighted round-robin distribution with health checking and failure recovery.

## Architecture

The system consists of:

- **Distributor**: HTTP service that receives log packets and routes them to analyzers
- **Analyzers**: Multiple analyzer services that process log packets
- **Emitter**: Load generator for testing the system

## Features

- **Weighted Round-Robin Routing**: Distributes packets to analyzers based on configurable weights
- **Health Checking**: Monitors analyzer health and automatically removes unhealthy analyzers
- **Failure Recovery**: Automatically re-adds analyzers when they become healthy again
- **Metrics**: Built-in metrics endpoint for monitoring distribution
- **Circuit Breaker**: Prevents cascading failures with configurable failure thresholds

We achieve the three requirements for the distributor in our design:

**High-throughput**: Achieved in the common case by decoupling network IO from routing and fast in-memory channels, but throughput can be limited by downstream analyzers, channel sizing, or our single-router bottleneck under extreme load.
**Non-blocking**: Mostly non-blocking for accept-paths (fast-path enqueue + buffered channels + short-block fallback). But the HTTP handler can block briefly waiting for enqueue confirmation (this is deliberate backpressure alleviation).
**Thread-safe distribution**: Achieved via channels, single-router model, and well-defined sender goroutines. Race conditions are minimized and counters are updated atomically.

## Quick Start

### Using Docker Compose (Recommended)

1. **Build and start all services (emitter, distributor, analyzers):**
   ```bash
   docker-compose up --build
   ```

2. **Check the status:**
   ```bash
   # Check distributor health
   curl http://localhost:8080/health
   
   # Check metrics
   curl http://localhost:8080/metrics
   ```

3. **Send test packets:**
   ```bash
   curl -X POST http://localhost:8080/ingest \
     -H "Content-Type: application/json" \
     -d '{
       "emitter_id": "test-emitter",
       "packet_id": "test-packet-1",
       "messages": [
         {
           "timestamp": 1640995200000,
           "level": "INFO",
           "message": "Test log message",
           "metadata": {"service": "test"}
         }
       ]
     }'
   ```
## Configuration

The distributor can be configured using environment variables:

### Server Configuration
- `PORT`: HTTP server port (default: 8080)
- `INGEST_BUFFER_SIZE`: Size of the ingest channel buffer (default: 10000)
- `PER_ANALYZER_QUEUE_SIZE`: Size of each analyzer's queue (default: 1000)

### Analyzer Configuration
- `ANALYZERS`: Comma-separated list of analyzers with weights
  - Format: `http://analyzer-1:9001=0.4,http://analyzer-2:9002=0.3,http://analyzer-3:9003=0.3`
  - Weights should sum to approximately 1.0

### Sender Configuration
- `SENDER_RETRIES`: Number of retries for failed requests (default: 3)
- `SENDER_TIMEOUT_MS`: HTTP request timeout in milliseconds (default: 500)

### Health Check Configuration
- `HEALTH_CHECK_INTERVAL_SEC`: Health check interval in seconds (default: 30)
- `MAX_FAILURES`: Maximum consecutive failures before marking analyzer unhealthy (default: 5)

## API Endpoints

### POST /ingest
Accepts log packets for distribution.

**Request Body:**
```json
{
  "emitter_id": "string",
  "packet_id": "string",
  "messages": [
    {
      "timestamp": 1640995200000,
      "level": "INFO",
      "message": "Log message content",
      "metadata": {
        "key": "value"
      }
    }
  ]
}
```

**Response:**
```json
{
  "status": "accepted",
  "packet_id": "packet-123"
}
```

### GET /health
Returns the health status of the distributor.

**Response:**
```json
{
  "status": "healthy",
  "healthy_analyzers": 3,
  "total_analyzers": 3,
  "timestamp": "2023-01-01T00:00:00Z"
}
```

### GET /metrics
Returns distribution metrics.

**Response:**
```json
{
  "total_packets": 1000,
  "packets_by_analyzer": {
    "analyzer-1": 400,
    "analyzer-2": 300,
    "analyzer-3": 300
  },
  "healthy_analyzers": 3,
  "total_analyzers": 3,
  "timestamp": "2023-01-01T00:00:00Z"
}
```

## Testing and Monitoring

### Load Testing
The included emitter can be used for load testing:

```bash
# High load test
./emitter --target=http://localhost:8080/ingest --rate=1000 --concurrency=50 --duration=60s

# Long-running test
./emitter --target=http://localhost:8080/ingest --rate=100 --concurrency=10
```

### Monitoring Distribution
Monitor the distribution of packets across analyzers:

```bash
# Check current metrics
curl http://localhost:8080/metrics | jq

# Watch metrics in real-time
watch -n 1 'curl -s http://localhost:8080/metrics | jq'
```

### Testing Failure Scenarios
1. **Stop an analyzer:**
   ```bash
   docker stop log-streamer_analyzer-1_1
   ```

2. **Check that traffic is redistributed:**
   ```bash
   curl http://localhost:8080/metrics
   ```

3. **Restart the analyzer:**
   ```bash
   docker start log-streamer_analyzer-1_1
   ```

4. **Verify it rejoins the rotation:**
   ```bash
   curl http://localhost:8080/health
   ```

## Performance Characteristics

- **Throughput**: Can handle thousands of packets per second
- **Latency**: Sub-millisecond packet routing
- **Memory**: Configurable buffer sizes for different memory constraints
- **Fault Tolerance**: Automatic recovery from analyzer failures

## Development

### Project Structure
```
├── cmd/
│   ├── distributor/     # Main distributor service
│   ├── analyzer/        # Sample analyzer service
│   └── emitter/         # Load generator
├── internal/
│   ├── api/            # HTTP handlers
│   ├── config/         # Configuration management
│   ├── health/         # Health checking
│   ├── model/          # Data models
│   ├── router/         # Packet routing logic
│   └── worker/         # Per-analyzer workers
├── docker-compose.yml  # Full system setup
└── Dockerfile*         # Container definitions
```

### Building
```bash
# Build all components
go build ./cmd/...

# Run tests
go test ./...

# Run with race detection
go test -race ./...
```

### Adding New Analyzers
1. Add the analyzer URL and weight to the `ANALYZERS` environment variable
2. Ensure the analyzer implements the required endpoints:
   - `POST /ingest` - Accepts log packets
   - `GET /health` - Returns health status

## License

This project is licensed under the MIT License.

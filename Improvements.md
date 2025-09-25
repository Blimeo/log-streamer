## Assumptions made in the distributor design

* Packets must remain atomic (no splitting) and are routed whole.
* Weights are static during a run (dynamic weights are a planned improvement).
* Emitters will honor 503 backpressure and implement retry/backoff.
* No persistent queue: if all analyzers are down or queues overflow, packets are rejected and emitters must retry.
* Number of analyzers is small, so O(N) routing is acceptable.

## Additional failure conditions to handle

* **Slow analyzers with healthy checks passing** — liveness probes alone miss saturation; add capacity-aware probes (queue depth / latency).
* **All analyzers down** — reject new packets quickly with 503 (Service Unavailable). Emit strong metrics/alerts so operators know data is being lost unless emitters retry.
* **Partial packet processing & duplicate deliveries** — enforce idempotency at analyzer side if retries can re-send same packet.
* **Long GC pauses or node starvation** - monitor distributor process health; bound channel sizes to cap memory use.
* **Malformed packets** — Validate payloads and drop invalid input safely.
* **Malicious / abusive emitters** — rate-limit at ingress.

## Improvements (by priority)

**Short-term, viable for MVP**

* WAL: Under the current implementation without WAL, the system provides **at-most-once delivery**: once a packet is accepted into an analyzer queue it will be delivered, but if all analyzers are unavailable or queues are saturated, packets are rejected. This keeps the design simple, low-latency, and predictable, but places responsibility on emitters to retry under backpressure. If stronger durability is required, adding persistence is the natural next evolution.

* Capacity-aware routing: bias against analyzers with high queue depth.
* Warm-up tokens on analyzer rejoin; exponential backoff for flapping analyzers.
* Backpressure hints: return `Retry-After` header and use `429` when rate-limited.

**Mid/long-term, for production environments**

* Dynamic weights with smooth redistribution.
* Prioritization/sampling: drop low-priority messages when overloaded.
* Admin APIs: drain analyzer, adjust weights, view queues.
* Sharded routers (consistent hashing) for horizontal scalability.
* Autoscaling analyzers from queue depth metrics.
* Multi-region deployment with geo-routing.
* Full observability (Prometheus, Grafana, tracing) + alert playbooks.

## Testing strategy

### Currently Implemented in test.sh

**Integration tests**
* End-to-end routing with real analyzers that succeed/fail
* Analyzer crash/restart; verify load redistribution and correct rejection codes
* Health checker detection of analyzer failures and recovery
* Weight redistribution when analyzers go offline

**Load & performance**
* Steady load with skewed packet sizes (5 and 20 messages per packet)
* Message-based routing algorithm verification (routes by message count, not packet count)
* Weighted distribution validation with mixed traffic patterns

**System validation**
* Service health checks for distributor and analyzers
* Packet ingestion endpoint testing
* Metrics collection and monitoring
* Docker Compose integration testing

### 🔄 Missing - Future Improvements

**Unit tests**
* Sender retry & circuit-breaker transitions
* Atomic counter invariants with enqueue failures

**Load & performance**
* Stress tests until analyzer queues fill; verify backpressure (503) returned promptly
* Latency testing under various load conditions
* Memory usage monitoring during high load

**Chaos / resilience**
* Add latency injection to simulate slow analyzers
* Simulate flapping analyzers (rapid start/stop cycles)
* Network partition testing
* Memory pressure and GC pause simulation

## Operational considerations

* **SLOs**: acceptable packet loss is only when backpressure occurs and emitter retries fail.
* **Alerts**: analyzer queue depths near full, high 503 rate, flapping analyzers.
* **Runbooks**: how to drain/replace analyzers, scale distributor.
* **Capacity planning**: size buffers and analyzer counts for expected QPS.

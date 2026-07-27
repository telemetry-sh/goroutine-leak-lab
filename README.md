# Goroutine Leak Lab

The request returned. The goroutine didn't.

This deterministic Go simulator demonstrates how stable request timeouts can
hide a growing population of abandoned workers. Every strategy receives the
same seeded request cohort:

- **Detached worker** ignores cancellation and later blocks sending to an
  abandoned result channel.
- **Context-aware worker** exits when the request context is canceled.
- **Buffered result** prevents a late send from blocking forever, but the
  expensive work still runs after the timeout.
- **Bounded pool** caps concurrency and exposes overload as queue delay and
  rejection.

The interactive interface is embedded into the Go binary. There is no Node.js
runtime, frontend framework, or external Go dependency.

## Run it

Requires Go 1.24 or newer.

```bash
go run ./cmd/server
```

Open [http://localhost:8080](http://localhost:8080). To use another port:

```bash
PORT=9090 go run ./cmd/server
```

The simulator is also available as JSON:

```bash
curl http://localhost:8080/api/simulate

curl -X POST http://localhost:8080/api/simulate \
  -H 'content-type: application/json' \
  -d '{
    "requestsPerSecond": 120,
    "timeoutMs": 100,
    "slowWorkPercent": 25,
    "slowWorkMs": 2200,
    "fastWorkMs": 45,
    "poolSize": 20,
    "queueSize": 60,
    "runSeconds": 90,
    "seed": 6842
  }'
```

## Verify it

```bash
make check
```

That runs formatting verification, `go vet`, the unit and HTTP tests, and the
race detector.

## Telemetry model

Request outcomes and worker lifecycles need to be correlated:

| Field | Why it matters |
| --- | --- |
| `request.id` | Joins the handler outcome to its worker |
| `request.timeout_ms` | Records when the caller stopped waiting |
| `work.context_canceled` | Proves cancellation reached the worker |
| `goroutine.state` | Separates active work from blocked channel sends |
| `goroutine.stack_hash` | Groups thousands of leaks by one creation site |
| `worker.result_delivery` | Distinguishes delivered, buffered, and abandoned results |
| `worker.pool_slot` | Connects work to a bounded concurrency policy |
| `runtime.goroutines` | Makes process-level growth visible over time |

A useful investigation starts by grouping long-lived goroutines by
`goroutine.stack_hash`, then joining that group back to the timed-out request
and its cancellation event.

## Model boundaries

This is an explanatory workload model, not a load generator or a live goroutine
profiler. Work duration, arrival order, queueing, and cancellation behavior are
seeded and simulated. The detached strategy models the common unbuffered-result
channel leak: a handler returns on its deadline, the worker finishes later, and
the worker blocks forever because nobody receives its result.

Production behavior depends on client disconnects, dependency cancellation
support, channel ownership, pool implementation, scheduler pressure, and the
actual stack paths captured by runtime profiles.

Built with Go's standard library, embedded HTML/CSS/JavaScript, and
[telemetry.sh](https://telemetry.sh) in mind. MIT licensed.

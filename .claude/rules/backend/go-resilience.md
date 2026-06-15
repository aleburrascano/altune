---
paths: ["services/go-api/**/*.go"]
---

# Go resilience

Every Go service that talks to the outside world -- databases, caches, HTTP APIs, queues, object storage -- must be designed for partial failure. The happy path is the exception; the rules below are the baseline.

### Timeouts

Every remote call gets an explicit timeout via `context.WithTimeout`. No exceptions.

```go
ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
defer cancel()

resp, err := client.Do(req.WithContext(ctx))
```

- HTTP clients: set `Timeout` on `http.Client` as a ceiling; use per-request `context.WithTimeout` for finer control
- Database: use `*Context` variants (`QueryContext`, `ExecContext`); set connection-level timeouts in the pool config
- Queues/caches: wrap every call in a context with a timeout appropriate to the operation
- gRPC: set per-call deadlines; propagate the incoming deadline to downstream calls
- Default to tight timeouts (1-5s) and loosen only with evidence. A missing timeout is a goroutine leak waiting to happen.

### Retries

- **Bounded count**: never retry indefinitely. 3 attempts is a sane default.
- **Exponential backoff with jitter**: prevents thundering herd on recovery. `baseDelay * 2^attempt + rand(0, baseDelay)`.
- **Idempotent operations only**: retrying a non-idempotent write (e.g., `POST /charge`) can double-charge. If the operation is not idempotent, do not retry -- or make it idempotent first (idempotency key).
- **Check `ctx.Done()` between attempts**: if the caller has cancelled or the deadline has passed, retrying is waste.

```go
for attempt := 0; attempt < maxRetries; attempt++ {
    if err := ctx.Err(); err != nil {
        return fmt.Errorf("context cancelled before attempt %d: %w", attempt, err)
    }
    err := doOperation(ctx)
    if err == nil {
        return nil
    }
    if !isRetryable(err) {
        return fmt.Errorf("non-retryable: %w", err)
    }
    backoff := baseDelay * time.Duration(1<<attempt)
    jitter := time.Duration(rand.Int63n(int64(baseDelay)))
    select {
    case <-time.After(backoff + jitter):
    case <-ctx.Done():
        return ctx.Err()
    }
}
return fmt.Errorf("exhausted %d retries: %w", maxRetries, lastErr)
```

### Ambiguous outcomes

A timeout does **not** prove nothing happened. The server may have committed the write before the client timed out. Handle ambiguous outcomes explicitly:

- If the operation has a natural idempotency key (order ID, request ID), query for the result before retrying.
- If it does not, treat the outcome as unknown and surface it to the caller rather than silently retrying.
- Log ambiguous outcomes with structured context (operation, attempt, timeout, correlation ID) so they can be investigated.

### Idempotency

Duplicate delivery is the norm for queues and webhooks. Guard with idempotency keys:

- Store a unique key (message ID, request ID) with the result. Before processing, check if the key already exists.
- The check-and-store must be atomic (DB transaction, conditional write) -- otherwise two concurrent deliveries can both pass the check.
- Design handlers to be safe to call twice with the same input: same result, no additional side effects.

### Circuit breakers

Unreliable dependencies should be wrapped in a circuit breaker:

- **Closed** (normal): requests flow through. Track failure rate.
- **Open** (tripped): requests fail fast without hitting the dependency. Reduces load on a failing service and prevents cascading failures.
- **Half-open** (probing): periodically allow one request through to test recovery.

Configure thresholds (failure rate, consecutive failures) based on observed behavior, not guesses. Start conservative (trip early) and relax with evidence.

### Bulkheads

Isolate failure domains so one misbehaving dependency cannot consume all resources:

- Separate connection pools per dependency (DB, cache, external API).
- Separate goroutine pools or semaphores for independent subsystems.
- A timeout on the cache client should not starve the DB connection pool.

### Backpressure

No unbounded buffering. Every queue, channel, and buffer must have a capacity limit.

- **Bounded queues**: use buffered channels with known capacity. When full, shed load (reject, return 503, apply backpressure upstream).
- **Admission control**: rate-limit inbound requests at the edge (`golang.org/x/time/rate`). Prefer server-side rate limiting over relying on clients.
- **Shedding**: when the system is overloaded, it is better to reject some requests cleanly (429/503) than to accept them all and respond slowly to everyone.
- A goroutine per request without a semaphore is unbounded. Use `errgroup.SetLimit(n)` or a semaphore channel.

### Crash consistency

What if the process dies after the side effect but before acknowledgement?

- **Database writes**: use transactions. If the process crashes mid-transaction, the DB rolls back.
- **Queue consumers**: do not ack the message until processing is complete and durable. If the process crashes before ack, the message is redelivered.
- **External API calls + local state**: if you call an external API and then update local state, a crash between the two leaves them inconsistent. Design for replay/recovery: either make the operation idempotent so replay is safe, or use an outbox pattern (write to local DB in the same transaction, publish asynchronously).
- Design every side-effecting operation to answer: "What happens if we crash right after this line?"

### Client disconnect

Propagate cancellation via context. When the client disconnects (HTTP request cancelled, gRPC stream closed), `ctx.Done()` fires.

- Long-running operations must select on `ctx.Done()` and clean up partial work.
- On `ctx.Err() == context.Canceled`: the client gave up. Log at Info level, clean up, return.
- On `ctx.Err() == context.DeadlineExceeded`: the deadline passed. Log at Warn level with the operation and elapsed time.
- Do not leave orphaned goroutines, open transactions, or half-written state behind.

### State consistency

Distributed state diverges. Plan for it:

- **DB/cache divergence**: cache invalidation is hard. Prefer short TTLs and cache-aside patterns over write-through when consistency matters. If the cache disagrees with the DB, the DB wins.
- **Publish/commit mismatch**: if you publish a message and then commit to the DB (or vice versa), a failure between the two creates inconsistency. Use transactional outbox: write the event to an outbox table in the same DB transaction, then publish from the outbox asynchronously.
- **External side effect succeeded but local state failed**: if you called a payment API successfully but then failed to record the payment locally, you have a dangling charge. Design compensation paths (refund, reconciliation job) or make the external call conditional on local state (reserve locally first, then call externally, then confirm locally).
- Accept that perfect consistency across boundaries is impossible. Design for eventual consistency with explicit reconciliation.

### Missing observability is a defect

If a resilience mechanism fires and nobody knows, it did not help. Every resilience path must be observable:

- **Structured logging**: log retries (attempt number, backoff duration, error), circuit breaker state transitions, timeout hits, idempotency dedup hits.
- **Correlation IDs**: propagate a request/trace ID through every retry, fallback, and compensation path so the full story is traceable.
- **Metrics**: expose counters/histograms for retry counts, circuit breaker trips, timeout rates, queue depth/lag, error rates by type. These are the first signals that something is degrading.

### The four questions

Before marking any code that touches a remote dependency as complete, ask:

1. **What happens if this dependency is slow?** (timeout + circuit breaker)
2. **What happens if this operation happens twice?** (idempotency)
3. **What happens if the process crashes mid-operation?** (crash consistency)
4. **What happens if the client disconnects?** (context cancellation + cleanup)

If any answer is "I don't know" or "it breaks", the code is not done.

### Tools

- **Serena MCP** for LSP operations: find references, go to definition, find implementations, rename symbol, get diagnostics.
- **context7 MCP** for latest Go standard library and third-party library documentation.

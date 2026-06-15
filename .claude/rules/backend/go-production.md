---
paths: ["services/go-api/**/*.go"]
---

# Go production: observability, context, security, resilience

## 1. Observability

Observability is the ability to understand a system's internal state from its external outputs. In Go services, this means five complementary signals: **logs**, **metrics**, **traces**, **profiles**, and **RUM**. Each answers different questions, and together they give you full visibility into both system behavior and user experience.

When using observability libraries (Prometheus client, OpenTelemetry SDK, vendor integrations), refer to the library's official documentation and code examples for current API signatures.

### Best practices

1. **Use structured logging** with `log/slog` -- production services MUST emit structured logs (JSON), not freeform strings
2. **Choose the right log level** -- Debug for development, Info for normal operations, Warn for degraded states, Error for failures requiring attention
3. **Log with context** -- use `slog.InfoContext(ctx, ...)` to correlate logs with traces
4. **Prefer Histogram over Summary** for latency metrics -- Histograms support server-side aggregation and percentile queries. Every HTTP endpoint MUST have latency and error rate metrics.
5. **Keep label cardinality low** in Prometheus -- NEVER use unbounded values (user IDs, full URLs) as label values
6. **Track percentiles** (P50, P90, P99, P99.9) using Histograms + `histogram_quantile()` in PromQL
7. **Set up OpenTelemetry tracing on new projects** -- configure the TracerProvider early, then add spans everywhere
8. **Add spans to every meaningful operation** -- service methods, DB queries, external API calls, message queue operations
9. **Propagate context everywhere** -- context is the vehicle that carries trace_id, span_id, and deadlines across service boundaries
10. **Enable profiling via environment variables** -- toggle pprof and continuous profiling on/off without redeploying
11. **Correlate signals** -- inject trace_id into logs, use exemplars to link metrics to traces
12. **A feature is not done until it is observable** -- declare metrics, add proper logging, create spans

### The five signals

| Signal | Question it answers | Tool | When to use |
| --- | --- | --- | --- |
| **Logs** | What happened? | `log/slog` | Discrete events, errors, audit trails |
| **Metrics** | How much / how fast? | Prometheus client | Aggregated measurements, alerting, SLOs |
| **Traces** | Where did time go? | OpenTelemetry | Request flow across services, latency breakdown |
| **Profiles** | Why is it slow / using memory? | pprof, Pyroscope | CPU hotspots, memory leaks, lock contention |
| **RUM** | How do users experience it? | PostHog, Segment | Product analytics, funnels, session replay |

### Correlating signals

Signals are most powerful when connected. A trace_id in your logs lets you jump from a log line to the full request trace. An exemplar on a metric links a latency spike to the exact trace that caused it.

#### Logs + Traces: `otelslog` bridge

```go
import "go.opentelemetry.io/contrib/bridges/otelslog"

// Create a logger that automatically injects trace_id and span_id
logger := otelslog.NewHandler("my-service")
slog.SetDefault(slog.New(logger))

// Now every slog call with context includes trace correlation
slog.InfoContext(ctx, "order created", "order_id", orderID)
// Output includes: {"trace_id":"abc123", "span_id":"def456", "msg":"order created", ...}
```

#### Metrics + Traces: Exemplars

```go
// When recording a histogram observation, attach the trace_id as an exemplar
// so you can jump from a P99 spike directly to the offending trace
obs := histogram.WithLabelValues("POST", "/orders")
if eo, ok := obs.(prometheus.ExemplarObserver); ok {
    eo.ObserveWithExemplar(duration, prometheus.Labels{"trace_id": traceID})
} else {
    obs.Observe(duration)
}
```

### Migrating legacy loggers

If the project currently uses `zap`, `logrus`, or `zerolog`, migrate to `log/slog`. It is the standard library logger since Go 1.21, has a stable API, and the ecosystem has consolidated around it.

**Migration strategy:**

1. Add `slog` as the new logger with `slog.SetDefault()`
2. Use bridge handlers during migration to route slog output through the existing logger
3. Gradually replace all legacy logger calls with `slog.Info(...)` etc.
4. Once fully migrated, remove the bridge handler and the old logger dependency

### Go 1.26+: slog multi-handler

For simple fan-out to multiple slog handlers, prefer stdlib `slog.NewMultiHandler` before adding third-party handler-composition dependencies.

```go
logger := slog.New(slog.NewMultiHandler(
    slog.NewJSONHandler(os.Stdout, nil),
    auditHandler,
))
```

### Definition of done for observability

A feature is not production-ready until it is observable. Before marking a feature as done, verify:

- [ ] **Metrics declared** -- counters for operations/errors, histograms for latencies, gauges for saturation. Each metric var has PromQL queries and alert rules as comments above its declaration.
- [ ] **Logging is proper** -- structured key-value pairs with `slog`, context variants used (`slog.InfoContext`), no PII in logs, errors MUST be either logged OR returned (NEVER both).
- [ ] **Spans created** -- every service method, DB query, and external API call has a span with relevant attributes, errors recorded with `span.RecordError()`.
- [ ] **Dashboards and alerts exist** -- the PromQL from your metric comments is wired into Grafana dashboards and Prometheus alerting rules.
- [ ] **RUM events tracked** -- key business events tracked server-side, identity key is `user_id` (not email), consent checked before tracking.

### Common mistakes

```go
// Bad -- log AND return (error gets logged multiple times up the chain)
if err != nil {
    slog.Error("query failed", "error", err)
    return fmt.Errorf("query: %w", err)
}

// Good -- return with context, log once at the top level
if err != nil {
    return fmt.Errorf("querying users: %w", err)
}
```

```go
// Bad -- high-cardinality label (unbounded user IDs)
httpRequests.WithLabelValues(r.Method, r.URL.Path, userID).Inc()

// Good -- bounded label values only
httpRequests.WithLabelValues(r.Method, routePattern).Inc()
```

```go
// Bad -- not passing context (breaks trace propagation)
result, err := db.Query("SELECT ...")

// Good -- context flows through, trace continues
result, err := db.QueryContext(ctx, "SELECT ...")
```

```go
// Bad -- using Summary for latency (can't aggregate across instances)
prometheus.NewSummary(prometheus.SummaryOpts{
    Name:       "http_request_duration_seconds",
    Objectives: map[float64]float64{0.99: 0.001},
})

// Good -- use Histogram (aggregatable, supports histogram_quantile)
prometheus.NewHistogram(prometheus.HistogramOpts{
    Name:    "http_request_duration_seconds",
    Buckets: prometheus.DefBuckets,
})
```

---

## 2. Context

`context.Context` is Go's mechanism for propagating cancellation signals, deadlines, and request-scoped values across API boundaries and between goroutines. Think of it as the "session" of a request -- it ties together every operation that belongs to the same unit of work.

### Best practices

1. The same context MUST be propagated through the entire request lifecycle: HTTP handler -> service -> DB -> external APIs
2. `ctx` MUST be the first parameter, named `ctx context.Context`
3. NEVER store context in a struct -- pass explicitly through function parameters
4. NEVER pass `nil` context -- use `context.TODO()` if unsure
5. `cancel()` MUST be called on all control-flow paths for `WithCancel`/`WithTimeout`/`WithDeadline`, unless ownership of the context and cancel function is explicitly returned or transferred
6. `context.Background()` MUST only be used at the top level (main, init, tests)
7. **Use `context.TODO()`** as a placeholder when you know a context is needed but don't have one yet
8. NEVER create a new `context.Background()` in the middle of a request path
9. Context value keys MUST be unexported types to prevent collisions
10. Context values MUST only carry request-scoped metadata -- NEVER function parameters
11. **Use `context.WithoutCancel`** (Go 1.21+) when spawning background work that must outlive the parent request

### Creating contexts

| Situation | Use |
| --- | --- |
| Entry point (main, init, test) | `context.Background()` |
| Function needs context but caller doesn't provide one yet | `context.TODO()` |
| Inside an HTTP handler | `r.Context()` |
| Need cancellation control | `context.WithCancel(parentCtx)` |
| Need a deadline/timeout | `context.WithTimeout(parentCtx, duration)` |

### Context propagation: the core principle

The most important rule: **propagate the same context through the entire call chain**. When you propagate correctly, cancelling the parent context cancels all downstream work automatically.

```go
// Bad -- creates a new context, breaking the chain
func (s *OrderService) Create(ctx context.Context, order Order) error {
    return s.db.ExecContext(context.Background(), "INSERT INTO orders ...", order.ID)
}

// Good -- propagates the caller's context
func (s *OrderService) Create(ctx context.Context, order Order) error {
    return s.db.ExecContext(ctx, "INSERT INTO orders ...", order.ID)
}
```

### Deep dives

- **Cancellation, Timeouts & Deadlines** -- How cancellation propagates: `WithCancel` for manual cancellation, `WithTimeout` for automatic cancellation after a duration, `WithDeadline` for absolute time deadlines. Patterns for listening (`<-ctx.Done()`) in concurrent code, `AfterFunc` callbacks, and `WithoutCancel` for operations that must outlive their parent request (e.g., audit logs).

- **Context Values & Cross-Service Tracing** -- Safe context value patterns: unexported key types to prevent namespace collisions, when to use context values (request ID, user ID) vs function parameters. Trace context propagation: OpenTelemetry trace headers, correlation IDs for log aggregation, and marshaling/unmarshaling context across service boundaries.

- **Context in HTTP Servers & Service Calls** -- HTTP handler context: `r.Context()` for request-scoped cancellation, middleware integration, and propagating to services. HTTP client patterns: `NewRequestWithContext`, client timeouts, and retries with context awareness. Database operations: always use `*Context` variants (`QueryContext`, `ExecContext`) to respect deadlines.

---

## 3. Security

### Security thinking model

Before writing or reviewing code, ask three questions:

1. **What are the trust boundaries?** -- Where does untrusted data enter the system? (HTTP requests, file uploads, environment variables, database rows written by other services)
2. **What can an attacker control?** -- Which inputs flow into sensitive operations? (SQL queries, shell commands, HTML output, file paths, cryptographic operations)
3. **What is the blast radius?** -- If this defense fails, what's the worst outcome? (Data leak, RCE, privilege escalation, denial of service)

### Severity levels

| Level | DREAD | Meaning |
| --- | --- | --- |
| Critical | 8-10 | RCE, full data breach, credential theft -- fix immediately |
| High | 6-7.9 | Auth bypass, significant data exposure, broken crypto -- fix in current sprint |
| Medium | 4-5.9 | Limited exposure, session issues, defense weakening -- fix in next sprint |
| Low | 1-3.9 | Minor info disclosure, best-practice deviations -- fix opportunistically |

### Quick reference

| Severity | Vulnerability | Defense | Standard Library Solution |
| --- | --- | --- | --- |
| Critical | SQL Injection | Parameterized queries separate data from code | `database/sql` with `?` placeholders |
| Critical | Command Injection | Pass args separately, never via shell concatenation | `exec.Command` with separate args |
| High | XSS | Auto-escaping renders user data as text, not HTML/JS | `html/template`, `text/template` |
| High | Path Traversal | Scope untrusted file access to an allowed root | Go 1.24+: use `os.Root`. Pre-1.24: `filepath.IsLocal` + `filepath.Rel` |
| Medium | Timing Attacks | Constant-time comparison avoids byte-by-byte leaks | `crypto/subtle.ConstantTimeCompare` |
| High | Crypto Issues | Use vetted algorithms; never roll your own | `crypto/aes`, `crypto/rand` |
| Medium | HTTP Security | TLS + security headers prevent downgrade attacks | `net/http`, configure TLSConfig |
| Low | Missing Headers | HSTS, CSP, X-Frame-Options prevent browser attacks | Security headers middleware |
| Medium | Rate Limiting | Rate limits prevent brute-force and resource exhaustion | `golang.org/x/time/rate`, server timeouts |
| High | Race Conditions | Protect shared state to prevent data corruption | `sync.Mutex`, channels, avoid shared state |

### Threat modeling (STRIDE)

Apply STRIDE to every trust boundary crossing and data flow: **S**poofing (authentication), **T**ampering (integrity), **R**epudiation (audit logging), **I**nformation Disclosure (encryption), **D**enial of Service (rate limiting), **E**levation of Privilege (authorization). Score each threat using DREAD to prioritize remediation.

### Research before reporting

Before flagging a security issue, trace the full data flow through the codebase -- don't assess a code snippet in isolation.

1. **Trace the data origin** -- follow the variable back to where it enters the system
2. **Check for upstream validation** -- look for input validation, sanitization, type parsing, or allow-listing earlier in the call chain
3. **Examine the trust boundary** -- if the data never crosses a trust boundary, the risk profile is different
4. **Read the surrounding code, not just the diff** -- middleware, interceptors, or wrapper functions may already provide defense

When downgrading or skipping a finding: add a brief inline comment (e.g., `// security: SQL concat safe here -- input is validated by parseUserID() which returns int`) so the decision is documented.

### Tooling & verification

```bash
# Go security checker (SAST)
go tool gosec ./...

# Vulnerability scanner
go tool govulncheck ./...

# Race detector
go test -race ./...

# Fuzz testing
go test -fuzz=Fuzz
```

### Common mistakes

| Severity | Mistake | Fix |
| --- | --- | --- |
| High | `math/rand` for tokens | Output is predictable. Use `crypto/rand` |
| Critical | SQL string concatenation | Attacker can modify query logic. Use parameterized queries |
| Critical | `exec.Command("bash -c")` | Shell interprets metacharacters. Pass args separately |
| High | Trusting unsanitized input | Validate at trust boundaries |
| Critical | Hardcoded secrets | Secrets in source code end up in version history. Use env vars or secret managers |
| Medium | Comparing secrets with `==` | `==` short-circuits on first differing byte. Use `crypto/subtle.ConstantTimeCompare` |
| Medium | Returning detailed errors | Stack traces help attackers. Return generic messages, log details server-side |
| High | Ignoring `-race` findings | Races cause data corruption and can bypass authorization. Fix all races |
| High | MD5/SHA1 for passwords | Known collision attacks, fast to brute-force. Use Argon2id or bcrypt |
| High | AES without GCM | ECB/CBC lack authentication. GCM provides encrypt+authenticate |
| Medium | Binding to 0.0.0.0 | Exposes to all interfaces. Bind to specific interface |

### Security anti-patterns

| Severity | Anti-Pattern | Why It Fails | Fix |
| --- | --- | --- | --- |
| High | Security through obscurity | Hidden URLs are discoverable via fuzzing, logs, or source | Authentication + authorization on all endpoints |
| High | Trusting client headers | `X-Forwarded-For`, `X-Is-Admin` are trivially forged | Server-side identity verification |
| High | Client-side authorization | JavaScript checks are bypassed by any HTTP client | Server-side permission checks on every handler |
| High | Shared secrets across envs | Staging breach compromises production | Per-environment secrets via secret manager |
| Critical | Ignoring crypto errors | `_, _ = encrypt(data)` silently proceeds unencrypted | Always check errors -- fail closed, never open |
| Critical | Rolling your own crypto | Custom encryption hasn't been analyzed by cryptographers | Use `crypto/aes` GCM, `golang.org/x/crypto/argon2` |

---

## 4. Resilience defaults

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

---

## 5. Tools

- **Serena MCP** for LSP operations: find references, go to definition, find implementations, rename symbol, get diagnostics.
- **context7 MCP** for latest Go standard library and third-party library documentation.

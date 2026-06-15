---
paths: ["services/go-api/**/*.go"]
---

# Go observability

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

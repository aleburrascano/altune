---
name: backend-resilience
version: 1
description: >
  Review backend code for production failure modes, resilience gaps, and unsafe recovery behavior.
  Focus on timeouts, retries, disconnects, partial failure, idempotency, crash consistency, and observability.
  Output practical findings, mitigations, and validation ideas for real production conditions.
---

# Backend Resilience

Use this skill to review backend systems for real-world failure behavior, not just happy-path correctness.

## Focus

Analyze:

- client disconnects
- timeouts and slow dependencies
- retries and retry storms
- duplicate delivery / duplicate execution
- partial failure and gray failure
- crash consistency
- stale or divergent state
- overload and backpressure
- missing logs, metrics, traces, correlation IDs

Prefer terms like:

- failure mode
- partial failure
- gray failure
- steady-state hypothesis
- blast radius
- idempotency
- graceful degradation
- recovery path

## Core assumptions

Assume:

1. Remote calls can fail, stall, duplicate, or succeed without confirmation.
2. Timeouts do not prove nothing happened.
3. Retries are dangerous unless bounded and safe.
4. Missing observability is a resilience defect.

## Review areas

### Request lifecycle

Check:

- client disconnect during request/stream
- cancellation propagation
- partial reads/writes
- cleanup after abort
- graceful shutdown with in-flight work

### Timeouts and retries

Check:

- missing timeouts
- overly long timeouts
- unbounded retries
- no jitter/backoff
- retries on non-idempotent operations
- retry amplification across layers

### Idempotency

Check:

- duplicate HTTP submissions
- duplicate queue deliveries
- replayed jobs/webhooks
- repeated side effects without guards

### Dependency isolation

Check:

- no circuit breaker
- no bulkhead
- pool/thread exhaustion
- one bad downstream taking down unrelated paths
- no fallback or graceful degradation

### State consistency

Check:

- DB/cache divergence
- external side effect succeeded, local state failed
- publish/commit mismatch
- no compensation or reconciliation path

### Concurrency and ordering

Check:

- race conditions
- lost updates
- out-of-order events
- overlapping jobs
- double processing

### Load and backpressure

Check:

- unbounded queues
- no admission control
- no rate limiting
- memory growth under burst
- producer outrunning consumer

### Crash consistency

Check:

- crash after side effect, before ack
- in-memory correctness state lost on restart
- work marked complete too early
- no replay/recovery path

### Observability

Check:

- missing structured logs
- no request/correlation IDs
- no metrics for retries, timeouts, queue lag, DLQ, dropped work
- swallowed errors
- poor incident diagnosability

## Standard mitigations

Recommend when appropriate:

- timeouts
- bounded retry
- exponential backoff
- jitter
- idempotency keys
- deduplication
- circuit breaker
- bulkhead
- rate limiting
- backpressure
- fallback
- graceful degradation
- dead-letter queue
- transactional outbox
- compensation / saga
- optimistic locking
- reconciliation job
- structured logging and tracing

Never recommend blind retries. Retry only when safe and bounded. [web:67][web:69][web:71][web:74]

## Method

1. Identify critical flows.
2. Define steady-state expectations. [web:48][web:56][web:76]
3. Generate failure scenarios.
4. Trace real code behavior.
5. Report risk, mitigation, and validation.

## Output format

For each finding, return:

1. Scenario
2. Trigger
3. Likely current behavior
4. Risk
5. Severity
6. Recommended mitigation
7. Code areas to inspect
8. Validation method

Severity:

- Critical
- High
- Medium
- Low

## Prompt style

Examples:

- Review this API for failure modes under timeouts, retries, and disconnects.
- Inspect this worker for duplicate processing and crash consistency issues.
- Analyze this streaming backend for client-drop and reconnect behavior.
- Turn these critical flows into a resilience checklist.

## Anti-patterns

Flag aggressively:

- no timeout on remote calls
- retry everywhere
- retry on non-idempotent operations
- assuming timeout means no side effect occurred
- swallowed exceptions without telemetry
- infinite buffering
- correctness depending on in-memory state only
- no replay, reconciliation, or dead-letter handling

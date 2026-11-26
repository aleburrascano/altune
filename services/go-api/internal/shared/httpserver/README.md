`httpserver` owns how HTTP requests are limited, logged, recovered and answered with errors.

## One error writer

`HandleServiceError(w, r, err)` is the only way a handler answers an error.

The answer is RFC 9457 `application/problem+json`, with `type` (`urn:altune:problem:<code>`), `title`, `status`, `detail`, `instance` (the correlation id, when the request has one) and `code`.

A handler never picks a status. The boundary reads `err`'s chain and takes the first of these that matches:

1. **The client went away:** the error is a cancellation and the request was cancelled too. The answer is 499 with no body.
2. **A coded error:** a `shared/domain` coded error is in the chain. Its code's kind sets the status (`kindStatus` in errors.go), and its message is the detail.
3. **Out of time:** the error is a deadline and the request's own deadline has passed. The answer is 504 `request.timeout`.
4. **Anything else** is 500 `internal`. Its detail never carries the cause.

**Logging:** each failure is logged once, here.

| Case | Message | Level |
| --- | --- | --- |
| The `request.timeout` code | `request.timed_out` | Warn |
| Any other 5xx, including a coded 504 | `request.failed` | Error |
| Any status below 500 | `request.rejected` | Info |
| A client that left | `request.cancelled` | Info |

A handler adds context by wrapping the error (`fmt.Errorf("...: %w", err)`), not by logging it. Separately, `RequestLogger` logs `request.complete` at Info for every request, including failed ones.

Write every error through `HandleServiceError`; the strict lint (`forbidigo`) bans `http.Error`.

## One measurement of each request

`ObserveRequests(now, observers...)` is the one middleware that measures a request: it reads the clock once at the start and once at the end, wraps the writer once, and hands every `Observer` the same `Outcome` (method, path, route template, status, bytes, duration, user). An observer may change the request on the way in (the span observer puts the span in its context) and returns a finisher, or nil to decline. Finishers run in reverse order, each guarded so a panic in one is logged and the rest still run. The span (`telemetry.RequestSpan`), the latency histogram (`requestmetrics.Observer`) and the `request.complete` log line (`LogRequest`) are three observers of it, so they share one route template. `RequestLogger(now)` is `ObserveRequests` with only the log observer.

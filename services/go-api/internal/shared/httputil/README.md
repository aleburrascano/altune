`httputil` owns how HTTP requests are limited, logged, recovered and answered with errors.

## One error writer

`HandleServiceError(w, r, err)` is the only way a handler answers an error.

The answer is RFC 9457 `application/problem+json`, with `type` (`urn:altune:problem:<code>`), `title`, `status`, `detail`, `instance` (the correlation id, when the request has one) and `code`.

A handler never picks a status. The boundary reads `err`'s chain and takes the first of these that matches:

1. **The client went away:** the error is a cancellation and the request was cancelled too. The answer is 499 with no body.
2. **A coded error:** a `shared/domain` coded error is in the chain. Its code's kind sets the status (`kindStatus` in errors.go), and its message is the detail.
3. **A status-carrying error:** an error with an `HTTPStatus()` method is in the chain. That status still maps until the last such types move to coded errors.
4. **Out of time:** the error is a deadline and the request's own deadline has passed. The answer is 504 `request.timeout`.
5. **Anything else** is 500 `internal`. Its detail never carries the cause.

**Logging:** each failure is logged once, here.

| Case | Message | Level |
| --- | --- | --- |
| The `request.timeout` code | `request.timed_out` | Warn |
| Any other 5xx, including a coded 504 | `request.failed` | Error |
| Any status below 500 | `request.rejected` | Debug |
| A client that left | `request.cancelled` | Info |

A handler adds context by wrapping the error (`fmt.Errorf("...: %w", err)`), not by logging it. Separately, `RequestLogger` logs `request.complete` at Info for every request, including failed ones.

`BadRequest`, `NotFound` and `InternalError` predate this boundary and still serve their last caller. The strict lint (`forbidigo`) bans them in new code, as it bans `http.Error`.

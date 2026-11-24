`httputil` owns how HTTP requests are limited, logged, recovered and answered with errors.

## One error writer

`HandleServiceError(w, r, err)` is the only way a handler answers an error.

- **The answer** is RFC 9457 `application/problem+json`, with `type` (`urn:altune:problem:<code>`), `title`, `status`, `detail`, `instance` (the correlation id) and `code`.
- **The status** comes from the kind of the `shared/domain` coded error in `err`'s chain; `kindStatus` in errors.go maps each kind. A handler never picks a status. Until the last status-carrying error types move to coded errors, an error with an `HTTPStatus()` method still maps through that method; a coded error wins over it.
- **The client went away:** if the error is a cancellation and the request was cancelled too, the answer is 499 with no body.
- **Out of time:** if the error is a deadline and the request's own deadline has passed, the answer is 504 `request.timeout`.
- **Anything else** is 500 `internal`. Its detail never carries the cause.
- **Logging:** each failure is logged once, here. A timeout is `request.timed_out` at Warn, any other 5xx is `request.failed` at Error, a 4xx is `request.rejected` at Debug, and a client that left is `request.cancelled` at Info. A handler adds context by wrapping the error (`fmt.Errorf("...: %w", err)`), not by logging it.

`BadRequest`, `NotFound` and `InternalError` predate this boundary and still serve their last caller. The strict lint (`forbidigo`) bans them in new code, as it bans `http.Error`.

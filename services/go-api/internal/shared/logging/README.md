`logging` owns how logs are formatted, keyed, redacted and kept in a ring buffer. Every log key a line carries is named in `keys.go`, and the correlation and trace ids are stamped on each line from its context, and so are the `job` and `run_id` that `background.RunJob` binds with `ContextWithJobRun` (an explicit `job` attr on the line wins).

## One vocabulary

A log key that has a `Key*` constant in `keys.go` is written with the constant, never as a string literal; the `rawLogKeys` rules in `_rules/logkeys.rules` (gocritic ruleguard, in the strict lint) reject the literal. A new key that two features share is added to `keys.go` and to the rule's list; `keys_test.go` fails when the rule lacks a constant.

Units: a duration is an integer number of milliseconds under `duration_ms` (`KeyDurationMS`), whatever it times; there is no `duration` key, no `time.Duration` value and no duration string. An error is always `error` (`KeyError`), never `err`. A track is `track_id`, a user `user_id`.

## Request context

`httpserver.RequestLogger` binds the user and the matched route into the request context with `ContextWithRequestFields`, and the handler stamps `user_id` and `route` on every line logged with that context (or one derived from it), the way it stamps `corr_id`. The user appears once the auth middleware has recorded it, so a line logged before authentication carries the route only. An explicit `user_id` or `route` attr on the line wins. So a request-scoped call site never attaches `user_id` by hand.

Work done for a user who is not the request user (a claimed job, a broadcast) binds that user once with `ContextWithSubjectUser(ctx, userID)` where the work starts; every line logged with that context then carries `user_id`, and the route of an enclosing request is kept. The subject user replaces the request user. A site never attaches `user_id` by hand there either.

## Capturing logs in a test

A test that asserts on log lines calls one of these, which swap the default logger and restore it in `t.Cleanup` (they take a `TB` with `Helper` and `Cleanup`, so this package imports no `testing`): `CaptureText(t, level)` and `CaptureJSON(t, level)` return a `*Capture` (an `io.Writer` with `String`, `Len`, `Reset`), `CaptureRing(t, level)` installs the production handler chain and returns its `*RingBuffer`, `Silence(t)` drops every line, and `UseHandler(t, h)` installs a handler of the test's own. A test never calls `slog.SetDefault`; the strict lint forbids it in test files too.

Only this package configures the default logger: `Setup` installs it at startup, and `Swap(l)` replaces it and returns the function that puts the previous one back, for a test that captures log lines (`t.Cleanup(logging.Swap(l))`). forbidigo bans `slog.SetDefault` everywhere else, and bans the plain `slog.Debug`, `Info`, `Warn` and `Error`, which carry no context and so lose the correlation id; log with `slog.InfoContext(ctx, ...)`.

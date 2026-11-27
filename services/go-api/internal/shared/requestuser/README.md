`requestuser` owns who a request comes from: its signed-in user, when their token expires, and its client address.

`LimitPerUser` is the one per-user rate limit middleware: each signed-in user gets their own bucket from the given limit, a refused request answers with the given error and `Retry-After`, and a request with no signed-in user passes through. Every handler that limits by user calls it instead of wiring a limiter and a user key itself.

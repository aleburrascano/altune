`requestuser` owns who a request comes from: its signed-in user, when their token expires, and its client address.

A handler behind the auth middleware reads its user with `UserID(r)`. The middleware is the only place that answers 401; `UserID` on a request that skipped it panics with `ErrNoSignedInUser`, so a route mounted outside the middleware fails closed (500) instead of acting for a zero user.

`LimitPerUser` is the one per-user rate limit middleware: each signed-in user gets their own bucket from the given limit, a refused request answers with the given error and `Retry-After`, and a request with no signed-in user passes through. Every handler that limits by user calls it instead of wiring a limiter and a user key itself.

`requestuser` owns who a request comes from: its signed-in user, when their token expires, and its client address.

A handler behind the auth middleware reads its user with `UserID(r)`. The middleware is the only place that answers 401; `UserID` on a request that skipped it panics with `ErrNoSignedInUser`, so a route mounted outside the middleware fails closed (500) instead of acting for a zero user.

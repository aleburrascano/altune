`auth` owns which bearer tokens are valid and which user each one names.

## Must not

- Read config or the environment: `app` passes the JWKS URL, project URL and audience.
- Own a table. The identity store is Supabase's, and `auth` only verifies its tokens.
- Map its errors anywhere but `internal/adapters/handler/errors.go`.

## Shape

The door is `auth.go`: the verifier, the middleware, the failure limits, the metrics and the revoker `app` wires together. Under `internal/`: `domain/` (`InvalidTokenError`, the reject reasons, `VerifiedToken`), `ports/ports.go` (`TokenVerifier`, `TokenRevoker`, `AuthMetrics`), `adapters/handler/` (the bearer middleware, the failure throttle and `errors.go` with the reason to `apierror` code map), `adapters/supabase/` (JWT and JWKS verification) and `adapters/telemetry/` (the counters). `authtest/` holds the metrics snapshot type and `ReasonSignatureInvalid` for `app`'s tests.

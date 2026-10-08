# Auth — Module Architecture

The auth bounded context (`services/go-api/internal/auth/`) verifies bearer tokens, gates HTTP requests on them and carries the request user identity to the rest of the backend.

- `domain/` — token vocabulary: `VerifiedToken`, `TokenRejectReason`, `InvalidTokenError` and `ErrTokenExpired`.
- `ports/` — what the module needs from outside: `TokenVerifier`, `AuthMetrics` and `TokenRevoker`.
- `service/` — the request user id and the token-expiry context, which other modules reach through their `adapters/authbridge/`.
- `adapters/` — `middleware/` is the HTTP gate with the throttle and `RequireUserID`; `providers/` verifies Supabase JWTs against the JWKS; `metrics/` counts outcomes in expvar; `testauth/` is the test-login verifier.

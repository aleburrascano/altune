# Auth — Module Architecture

The auth bounded context (`services/go-api/internal/auth/`) verifies bearer tokens and gates HTTP requests on them. It puts the verified user and token expiry on the request context through `internal/shared/requestuser`, which is where the rest of the backend reads them.

- `domain/` — token vocabulary: `VerifiedToken`, `TokenRejectReason`, `InvalidTokenError`, plus `ErrTokenExpired`, an alias of `requestuser.ErrTokenExpired`.
- `ports/` — what the module needs from outside: `TokenVerifier`, `AuthMetrics` and `TokenRevoker`.
- `service/` — aliases of the `requestuser` context functions, kept until the last caller imports `requestuser` itself.
- `adapters/` — `middleware/` is the HTTP gate with the throttle (`RequireUserID` and `ClientKey` there are aliases of `requestuser`); `providers/` verifies Supabase JWTs against the JWKS; `metrics/` counts outcomes in expvar; `testauth/` is the test-login verifier.

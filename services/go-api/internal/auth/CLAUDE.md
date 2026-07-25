# Auth context — router

Supabase JWT verification middleware. Not a full hexagon — one port (`TokenVerifier`), one middleware, one adapter (`SupabaseJWTVerifier` over a `jwk.Cache`).

Layout:

- `verifier.go` — `TokenVerifier`, `VerifierFunc`, `TokenRejectReason`, `InvalidTokenError`.
- `middleware.go` — `Middleware`, `RequireUserID`, `UserIDFromContext`, the reject helpers.
- `adapters/supabase_jwt.go` — `SupabaseJWTVerifier`, `classifyJWTError`.

## Rules

- Read the user id only through `RequireUserID` / `UserIDFromContext` — never touch the raw context key.
- Extend `TokenRejectReason` for a new rejection case; never emit free-text.
- Keep JWT internals in the adapter — the middleware must know nothing about them.
- A verifier that could not run is a 503, never a 401.
- Never rename `TestSupabaseJWTVerifier_InvalidSignature` or `_UnknownKeyID` — they pin the jwx message substrings.

Why each rule exists: `okf/backend/auth.md` — read before structural work; update it in the same commit when behavior it describes changes (pre-commit hook enforces).

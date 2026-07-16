# Auth context — router

Supabase JWT verification middleware. Not a full hexagon — one port (`TokenVerifier`), one middleware, one adapter (`SupabaseJWTVerifier` over a jwk.Cache).

Invariants:

- Every downstream handler gets the user id via `auth.RequireUserID` / `UserIDFromContext`; the context key is an unexported type — never read the raw context key elsewhere.
- `TokenRejectReason` is a closed vocabulary (`missing`, `malformed`, `signature_invalid`, `expired`, `claim_invalid_*`); new rejection cases extend it, never free-text.
- The middleware knows nothing about JWT internals — JWT classification stays in the adapter (`classifyJWTError`).

Knowledge base: `okf/backend/auth.md` — read before structural work; update it in the same commit when behavior it describes changes (pre-commit hook enforces).

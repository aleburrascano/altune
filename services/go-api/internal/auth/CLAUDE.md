# Auth context — router

Supabase JWT verification middleware. Not a full hexagon — one port (`TokenVerifier`), one middleware, one adapter (`SupabaseJWTVerifier` over a jwk.Cache).

Invariants:

- Every downstream handler gets the user id via `auth.RequireUserID` / `UserIDFromContext`; the context key is an unexported type — never read the raw context key elsewhere.
- `TokenRejectReason` is a closed vocabulary (`missing`, `malformed`, `signature_invalid`, `expired`, `claim_invalid_*`); new rejection cases extend it, never free-text.
- The middleware knows nothing about JWT internals — JWT classification stays in the adapter (`classifyJWTError`).
- A verifier that could not run (JWKS unreachable) is a 503, never a 401 — `rejectVerifierUnavailable`. Only an `InvalidTokenError` means the token was actually judged and rejected; an IdP outage must not read as "everyone got logged out".
- `hasUntypedSignatureFailure` matches jwx message text because jwx v2 exposes no typed sentinel for signature failures. The substrings are pinned by `TestSupabaseJWTVerifier_InvalidSignature` and `TestSupabaseJWTVerifier_UnknownKeyID`, which run the real library — a jwx upgrade that rewords them fails those tests instead of silently reclassifying signature failures as malformed.

Knowledge base: `okf/backend/auth.md` — read before structural work; update it in the same commit when behavior it describes changes (pre-commit hook enforces).

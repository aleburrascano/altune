---
type: Bounded Context
title: Auth
description: Supabase-issued JWT verification middleware that authenticates every inbound request and injects the verified user id into context.
resource: services/go-api/internal/auth/
tags: [bounded-context, hexagonal, go-api, auth, jwt, middleware, supabase]
verified_commit: 6a047a008fb23b38e719d9a9a3e9b539ab349d4d
---

Auth is a small cross-cutting context, not a full hexagon with its own domain/service split — it's a chi middleware plus one adapter. It authenticates every request via a Supabase-issued JWT and threads the verified user id through `context.Context` for downstream handlers.

**Port** (`verifier.go`): `TokenVerifier{Verify(ctx, token string) (shared.UserId, error)}` — the single port, satisfied by the Supabase adapter (or a test double). `TokenRejectReason` is a closed string-enum vocabulary (`missing`, `malformed`, `signature_invalid`, `expired`, `claim_invalid_iss`, `claim_invalid_aud`, `claim_invalid_sub`) carried on `InvalidTokenError{Reason, Detail}` so the middleware can report a precise, machine-readable rejection reason without the middleware itself knowing JWT internals.

**Middleware** (`middleware.go`): `Middleware(verifier TokenVerifier) func(http.Handler) http.Handler` is the Chain-of-Responsibility entry point — the standard `net/http`/chi middleware shape used throughout the codebase (see `.claude/rules/design-patterns/behavioral/chain-of-responsibility.md`). It extracts the `Authorization: Bearer <token>` header, calls `verifier.Verify`, and on failure walks the error chain with `errors.As` to recover the specific `InvalidTokenError.Reason` (falling back to `ReasonSignatureInvalid` if the error isn't of that type) before writing a 401 with `{detail, reason}` JSON and a `WWW-Authenticate: Bearer` header (`rejectToken`). On success it stores the verified `shared.UserId` in the request context under an unexported `contextKey{}` type (collision-proof per Go convention) and calls `next.ServeHTTP` with the enriched context.

Two context helpers are exported for downstream consumers: `UserIDFromContext(ctx) (shared.UserId, bool)` for optional lookups, and `RequireUserID(w, r) (shared.UserId, bool)` — the one every handler across catalog/acquisition/playback actually calls; it writes a 401 itself and returns `ok=false` if somehow unauthenticated (defense in depth — the middleware should already have rejected the request).

**Adapter** (`adapters/supabase_jwt.go`): `SupabaseJWTVerifier` wraps a `jwk.Cache` (from `lestrrat-go/jwx/v2`) that auto-refreshes Supabase's JWKS endpoint. `Verify` parses and validates the token (signature via the cached key set, issuer = `<projectURL>/auth/v1`, audience, ±5s clock skew), then extracts the JWT `sub` claim and parses it as a `shared.UserId` (`shared.ParseUserId`) — an empty or malformed `sub` is rejected as `ReasonClaimInvalidSUB`. `classifyJWTError` maps jwx's typed errors (`jwt.ErrTokenExpired`, `ErrInvalidIssuer`, `ErrInvalidAudience`) and message-substring fallbacks (`"failed to find key"`, `"could not verify message"`) onto the `TokenRejectReason` vocabulary, defaulting to `ReasonMalformed`.

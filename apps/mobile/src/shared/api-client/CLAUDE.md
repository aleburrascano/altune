# shared/api-client — router

Typed HTTP client for go-api: `apiFetch<T>` base wrapper + per-context typed function files.

Invariants:

- `apiFetch` is the single fetch wrapper — every typed function goes through it (auth header injection per ADR-0006, `ApiError` on non-2xx, `204`/`304` → `undefined`).
- A missing/errored Supabase session **fails fast**: `apiFetch` throws `ApiError(401)` before any network request (`getSession` resolves with `{session: null, error}` — read the `error` field, it never throws). A *server* 401 additionally calls `markSessionExpired()` so `AuthGate` can offer re-auth; a 500 never marks it.
- Wire types are hand-maintained (`types.ts` flags the sync risk) — a backend response-shape change must update them in the same change.
- Enrichment responses follow the null-object contract: collections always present, unresolved entity = empty payload.

Knowledge base: `okf/mobile/shared-api-client.md` — read before structural work; update in the same commit when behavior it describes changes (pre-commit hook enforces).

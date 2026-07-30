# shared/api-client — router

Typed HTTP client for go-api: `apiFetch<T>` base wrapper + `errors.ts` / `deadline.ts` + per-context typed function files.

Invariants:

- `apiFetch` is the single fetch wrapper — every typed function goes through it (auth header injection per ADR-0006, `ApiError` on non-2xx, `202`/`204`/`304` → `undefined`).
- A missing/errored Supabase session **fails fast**: `apiFetch` throws `ApiError(401)` before any network request (`getSession` resolves with `{session: null, error}` — read the `error` field, it never throws). A *server* 401 additionally calls `markSessionExpired()` so `AuthGate` can offer re-auth; a 500 never marks it.
- A transport failure is never an `ApiError`. `NetworkError` covers a failed session refresh (`error.name === 'AuthRetryableFetchError'`), an unreachable host, a timeout and a truncated body; only these plus `429`/`5xx` are retryable.
- Every request gets a deadline via `startDeadline` — never call `fetch` without one.
- A caller's `AbortError` is rethrown as-is, never relabelled `NetworkError`.
- Retry policy lives only in the QueryClient predicate (`isRetryable`) — never add a retry loop inside `apiFetch`.
- Wire types are hand-maintained (`types.ts` flags the sync risk) — a backend response-shape change must update them in the same change.
- Enrichment responses follow the null-object contract: collections always present, unresolved entity = empty payload; `has_content` is the server's verdict on whether a section is worth rendering.
- `feedback.ts` is the write side of in-app reports: `submitReport` POSTs to `/v1/feedback/reports`, which 404s on a deploy with no issue tracker configured and never throttles a reporter.
- `library.ts` is the read side of the collection: `/v1/library/albums` and `/v1/library/artists` return server-grouped lenses, and `getTracks` takes `q` / `sort`.

Knowledge base: `okf/mobile/shared-api-client.md` — read before structural work; update in the same commit when behavior it describes changes (pre-commit hook enforces).

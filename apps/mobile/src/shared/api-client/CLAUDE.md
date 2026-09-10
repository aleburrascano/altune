# shared/api-client — router

Typed HTTP client for go-api: `apiFetch<T>` base wrapper + `errors.ts` / `deadline.ts` + `parse.ts` (boundary narrowing) + per-context typed function files.

Invariants:

- `apiFetch` is the single fetch wrapper — every typed function goes through it (auth header injection per ADR-0006, `ApiError` on non-2xx, `202`/`204` → `undefined`).
- A migrated read parses its wire body through `parse.ts` (`apiFetch<unknown>` → per-type parser) and returns the domain type; a malformed/off-contract/old-server body is a `ContractError`, never an unguarded `as T`.
- Narrow a wire field in `parse.ts` field by field including union membership; a required field's absence or wrong type throws, an optional/legacy field defaults, and no downstream code re-parses.
- `parse.ts` closes the high-traffic reads (search, library tracks/albums/artists, `createTrack`); the remaining endpoints still cast and are migrated as they are touched.
- A non-2xx reads its JSON error body through `parseErrorBody` and threads the optional `code` onto `ApiError.code`; callers branch on `error.code`, never the message text, and a bodyless/non-JSON error still yields a status-only `ApiError`.
- A missing/errored Supabase session **fails fast**: `apiFetch` throws `ApiError(401)` before any network request (`getSession` resolves with `{session: null, error}` — read the `error` field, it never throws). A *server* 401 additionally calls `markSessionExpired()` so `AuthGate` can offer re-auth; a 500 never marks it.
- A transport failure is never an `ApiError`. `NetworkError` covers a failed session refresh (`error.name === 'AuthRetryableFetchError'`), an unreachable host, a timeout and a truncated body; only these plus `429`/`5xx` are retryable.
- Every request gets a deadline via `startDeadline` — never call `fetch` without one.
- Interpolate a path segment through `encodeURIComponent`; build every query string with `URLSearchParams`.
- A caller's `AbortError` is rethrown as-is, never relabelled `NetworkError`.
- Retry policy lives only in the QueryClient predicate (`isRetryable`) — never add a retry loop inside `apiFetch`.
- Wire types are hand-maintained (`types.ts` flags the sync risk) — a backend response-shape change must update them in the same change.
- Enrichment responses follow the null-object contract: collections always present, unresolved entity = empty payload; `has_content` is the server's verdict on whether a section is worth rendering.
- `feedback.ts` is the write side of in-app reports: `submitReport` POSTs to `/v1/feedback/reports`, which 404s on a deploy with no issue tracker configured and never throttles a reporter.
- `library.ts` is the read side of the collection: `/v1/library/albums` and `/v1/library/artists` return server-grouped lenses, and `getTracks` takes `q` / `sort`.
- `playlists.ts` writes membership only in batches — `POST …/tracks/batch` and `DELETE …/tracks` both take a `track_ids` list and answer with counts, never a bare 204.
- `tracks.ts` exposes `getAllTracks` for callers that need the whole collection in server order; it pages until `has_more` is false and is never used to render a list.
- `favorites.ts` identifies a Favorite only by the server's `favorite_key` — never derive that key on the device.

Tests: `__tests__/` — `apiFetch.auth`, `transport`, `deadline`, `isRetryable`, `tracks`, `playlists`, `library`, `discovery`, `favorites`, `queryString.property`, `enrichment`, `lyrics`, `audio`, `playback`, `feedback`, `contract`, `invariants`, `parse`.

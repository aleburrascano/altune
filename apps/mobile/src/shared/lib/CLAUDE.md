# shared/lib — feature-local router

Pure utilities promoted here only once a second feature needed them, plus the one in-memory seam between discover/library and detail.

Layout:

- `query-keys.ts` — `libraryKeys`/`discoveryKeys`/`playlistKeys`, the single declaration of the React Query cache topology.
- `detail-handoff.ts` — the discover↔detail handoff: the tapped `DiscoveryResult` and its originating `search_id`.
- `featured.ts` — `featuredArtistsFromExtras` (wire parser) and `withFeaturing` (the co-billed display line).
- `track-to-discovery.ts` — `trackToDiscoveryResult`, adapting a saved `TrackResponse` into the discovery wire shape.
- `async-view.ts` — `asyncView`, the shared *loading > error > empty > ready* precedence.
- `format.ts` — `formatDuration` (m:ss). `isNetworkError.ts` — the transport-failure classifier.
- `__tests__/` — `query-keys.test.ts`, `detail-handoff.test.ts`, `featured.test.ts`, `featuredContract.test.ts`, `track-to-discovery.test.ts`, `async-view.test.ts`, `format.test.ts`, `isNetworkError.test.ts`, `slice-invariants.test.ts`.

Dependencies: type-only imports from `@shared/api-client/{types,discovery}`. Nothing else — no React, no I/O, no feature imports.

## Rules

- Keep every module here pure: no `react`, no `react-native`, no `expo-*`, no `@tanstack/react-query`, and no runtime import from `@shared/api-client` — type-only imports are permitted.
- Promote a module here only when 2+ distinct features consume it.
- Declare every query key here; never retype a key literal at a call site.
- Give every key factory a matching `<x>Prefix` when a caller needs to invalidate the family.
- Keep `libraryKeys.summary` outside `tracksPrefix` — the SSE patch layer treats everything under that prefix as `InfiniteData`.
- Assert query keys by literal shape in tests; comparing a key against the same imported constant constrains nothing.
- Narrow every value coming out of an `extras` map before use, and reject non-finite numbers rather than trusting `typeof`.
- Never render server-mutable state from the detail handoff — read it once for identity, then subscribe to the owning store or cache.
- Never assume a wire string is non-empty: an empty `artwork_url` must collapse to `null`, not reach an image request.
- `slice-invariants.test.ts` enumerates this directory from disk — a new file here is automatically subject to the purity, 2+-consumer and banned-noun rules.

Why each rule exists: `okf/mobile/shared-lib.md`; the test selection is `okf/testing/shared-lib.md` — read before structural work, update in the same commit when behavior changes (pre-commit hook enforces).

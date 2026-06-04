# detail — feature-local context

Read-only detail screen for a tapped discovery result (`view-result-detail` spec). Fed by an in-memory handoff — no per-item backend fetch. A track can be saved to the library with an optimistic UI; album/artist show placeholder bodies and no Save. Lateral navigation (AC#11-13) allows tapping artist/album names to browse related content.

## Key terms

- **Handoff** — the last-tapped `DiscoveryResult`, stashed in `@shared/lib/detail-handoff` (shared, not feature-local: discover writes, detail reads). DetailScreen reads it on mount; an empty handoff (cold start / reload / deep link) redirects to `/discover`.
- **AcquisitionStatus pending** — a freshly saved track starts `pending` (audio not yet acquired); the library row shows a "Pending" marker.

## Patterns specific here

- **Pure helpers, thin JSX** (same as discover/library). `extras.ts` (`trackInfoRows`, `formatDuration`) and `save-cache.ts` (`insertOptimisticTrack`, `optimisticTrack`, `toCreateTrackRequest`) hold the logic and are unit-tested without rendering; `DetailScreen.tsx` is the wrapper.
- **Primitives imported directly** (`@shared/ui/primitives/*`), not the barrel, to keep jest transitive loads small. `Artwork` → `expo-image` is mocked in the component test.
- **Optimistic save.** `useSaveTrack` prepends a pending placeholder to the `['library']` infinite-query cache on mutate, rolls back to the snapshot on error, and invalidates on settle so a dedup hit reconciles to the server row. Cache transforms are pure (`save-cache.ts`).
- **`extras` is an untyped wire map.** Every key is narrowed before use; absent/empty values are omitted. Track keys verified against the deezer/itunes/musicbrainz/soundcloud adapters: `duration_seconds`, `album`, `isrc`, `popularity`.
- **Save guarded by the Track artist invariant** — when `result.subtitle` (artist) is null the Save button is disabled and `onSave` short-circuits (no invalid POST).
- **Lateral navigation via search** — `useLateralNav` hook searches for artist/album by name (`searchDiscovery` with `limit: 1`) and navigates via `router.replace('/detail')` (not push) to keep the back stack shallow. Artist name is tappable on track/album detail; album row is tappable on track detail when `extras['album']` exists.

## TestIDs (load-bearing)

`detail-header`, `detail-back`, `detail-artist-link` (tappable artist name), `detail-track-info`, `detail-info-<key>` (duration/album/isrc/popularity — album is tappable), `detail-save`, `detail-save-error`, `detail-tracklist-placeholder` (album), `detail-discography-placeholder` (artist).

## Routing

`src/app/detail.tsx` is a **sibling** of `(tabs)`, so it mounts in the root `_layout` `<Slot/>` (inside `AuthGate`) and the tab bar is hidden. Navigation: `DiscoverScreen.onResultTap` calls `stashHandoffForDetail(result)` then `router.push('/detail')`; the click-tracking mutation stays fire-and-forget. Adding a new route under `app/` requires regenerating `.expo/types/router.d.ts` (run `expo start` once) before `tsc` accepts the typed `href`.

## Dependencies

- `@shared/lib/detail-handoff` — the discover↔detail seam.
- `@shared/api-client/tracks` (`createTrack`) + `types` (`CreateTrackRequest`, `TrackResponse`).
- `@shared/api-client/discovery` (`DiscoveryResult`).
- `@tanstack/react-query` — `useSaveTrack` mutation, via the root `QueryClientProvider`.
- `@shared/ui/primitives/*` — `Screen`, `Text`, `Artwork`, `Button`, `Banner`.
- No cross-feature imports (vertical-slice rule).

## Test files

- `__tests__/extras.test.ts` — `trackInfoRows` / `formatDuration`.
- `__tests__/save-cache.test.ts` — optimistic cache transforms + request mapping.
- `__tests__/useSaveTrack.test.ts` — optimistic insert + rollback against a real QueryClient.
- `__tests__/useLateralNav.test.ts` — search-and-navigate for lateral browsing.
- `__tests__/DetailScreen.test.tsx` — header, redirect, per-kind bodies, Save press + disable, lateral navigation links.

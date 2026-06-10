# detail — feature-local context

Read-only detail screen for a tapped discovery result (`view-result-detail` spec). Fed by an in-memory handoff — no per-item backend fetch. A track can be saved to the library with an optimistic UI. Album detail shows tracklist fetched from provider API; artist detail shows top tracks + albums. Lateral navigation (AC#11-13) allows tapping artist/album names to browse related content; content items are tappable to navigate deeper (AC#14-20).

## Key terms

- **Handoff** — the last-tapped `DiscoveryResult`, stashed in `@shared/lib/detail-handoff` (shared, not feature-local: discover writes, detail reads). DetailScreen reads it on mount; an empty handoff (cold start / reload / deep link) redirects to `/discover`.
- **AcquisitionStatus pending** — a freshly saved track starts `pending` (audio not yet acquired); the library row shows a "Pending" marker.

## Patterns specific here

- **Pure helpers, thin JSX** (same as discover/library). `extras.ts` (`trackInfoRows`, `formatDuration`) and `save-cache.ts` (`insertOptimisticTrack`, `optimisticTrack`, `toCreateTrackRequest`) hold the logic and are unit-tested without rendering; `DetailScreen.tsx` is the wrapper.
- **Primitives imported directly** (`@shared/ui/primitives/*`), not the barrel, to keep jest transitive loads small. `Artwork` → `expo-image` is mocked in the component test.
- **Optimistic save.** `useSaveTrack` prepends a pending placeholder to the `['library']` infinite-query cache on mutate, rolls back to the snapshot on error, and invalidates on settle so a dedup hit reconciles to the server row. Cache transforms are pure (`save-cache.ts`). The optimistic placeholder includes null values for the extended metadata fields (`year`, `genre`, `track_number`, `album_artist`, `isrc`, `audio_ref`) added by the `import-legacy-library` spec.
- **`extras` is an untyped wire map.** Every key is narrowed before use; absent/empty values are omitted. Track detail shows `duration_seconds`, `album`, and `featured_artists` (ISRC and popularity removed — not user-facing per Spotify parity). Featured artists come from MB's `artist-credit` array (structured) OR from regex parsing of "feat./ft./with" in the title/subtitle strings (fallback for Deezer/iTunes/SC). `formatDuration` promoted to `@shared/lib/format` (2-consumer rule); re-exported from `extras.ts` for backwards compatibility.
- **Save guarded by the Track artist invariant** — when `result.subtitle` (artist) is null the Save button is disabled and `onSave` short-circuits (no invalid POST).
- **Lateral navigation via search** — `useLateralNav` hook searches for artist/album by name (`searchDiscovery` with `limit: 1, saveHistory: false`) and navigates via `router.push` within the current tab's stack. Uses a `searchingRef` guard that **never resets after a successful push** — prevents duplicate detail screens. The new detail screen gets its own fresh instance. Artist name is tappable on track/album detail; album row and featured artist names are also tappable. Shows Banner on failure (not Alert).
- **Album content fetch** — `useAlbumTracks` hook calls `getAlbumTracks(provider, externalId)` using the first source from the album's `sources[]`. If a MusicBrainz source also exists, fetches MB tracks in parallel and merges `featured_artists` by title match (`_mergeFeaturing`). React Query cached per `(provider, external_id)` with 30min staleTime. Track rows are tappable → navigate to track detail; tapped tracks inherit the parent album's `image_url` when they lack their own.
- **Featured artists (three-tier)** — (1) Deezer `/track/{id}` contributors (primary, fetched per-track in `_enrich_contributors`), (2) MB `artist-credit[1:]` (fallback via cross-reference), (3) regex parsing of "feat./ft./with" from title/subtitle strings. `trackInfoRows` renders a "Featuring" info row; album tracklist uses `_trackSubtitleWithFeaturing` to append featured names to the artist subtitle. Featured names are tappable links (lateral nav to artist).
- **Artist content fetch (MB + Deezer union)** — `useArtistContent` hook accepts `sources` + `mbid` (the artist's authoritative MBID from `extras.mbid`, resolved by the backend). The MB source is picked by `external_id === mbid` first (the merged card can carry several same-name MusicBrainz artists — 8 for "Che"), falling back to the first MB source. MB and Deezer albums are fetched via two React Query calls (`limit: 100`), union'd with `dedupAlbumsByTitle` (`normalizeForDedup` key, keep highest `track_count`, merge sources), sorted by `release_date`/`year` descending. **MB-authoritative filter**: when the identity is verified (`mbid` matches the queried MB source) and MB returned a non-empty list, Deezer only enriches title-matched albums and contributes no new titles — Deezer's artist entities conflate same-name artists (Che's list mixes unrelated 1990s/German/Spanish releases) and its album entries carry no artist field to filter on. Unverified artists keep the full union. **Per-provider failure handling**: the backend reports provider failures as HTTP 200 with `status: 'timeout'/'error'` + empty items — the hook treats any non-ok payload as that provider's failure, never surfaces its items, and `isErrorAlbums` fires only when every available provider failed (one healthy provider still renders partial data, no error).
- **Discography sections** — `DiscographySections` component groups albums by `extras.record_type` into Albums, Singles, EPs (compilations grouped under Albums). Each section renders only if the artist has releases of that type. Each is a horizontal scroll row.
- **Album metadata footer** — `AlbumDetailBody` shows "year · N tracks · duration" as a centered footer below the tracklist. Duration formatted as "1 hr 12 min" or "45 min".
- **Album card text** — left-aligned (no `textAlign: 'center'`), title wraps naturally, year + track count below.
- **Single-scroll layout** — album and artist detail wrap hero + content in one `ScrollView` (no nested scroll). Track detail has no scroll (content is short). This prevents the nested-scroll UX antipattern where the user has to scroll in a small area.
- **Sticky back button** — the back button is rendered OUTSIDE the ScrollView so it stays visible when scrolling. `router.canGoBack()` checked before `router.back()`; if false, navigates to the tab root (`/discover` or `/library` via `useSegments`).
- **Accessibility** — all tappable elements have `accessibilityRole` + `accessibilityLabel`: back button, artist/album links, track rows, album cards. Touch targets ≥48pt.

## TestIDs (load-bearing)

**Track detail:** `detail-header`, `detail-back`, `detail-artist-link` (tappable artist name), `detail-track-info`, `detail-info-<key>` (duration/album/isrc/popularity — album is tappable), `detail-save`, `detail-save-error`.

**Album detail:** `detail-tracklist-loading`, `detail-tracklist-error`, `detail-tracklist-empty`, `detail-tracklist` (success), `detail-track-<n>` (each track row, 0-indexed).

**Album metadata:** `detail-album-meta` (year · tracks · duration summary).

**Artist detail:** `detail-artist-content` (container), `detail-top-tracks-loading`, `detail-top-tracks-error`, `detail-top-track-<n>` (each top track), `detail-albums-loading`, `detail-albums-error`, `detail-album-<n>` (each album card), `detail-single-<n>` (each single card), `detail-ep-<n>` (each EP card).

## Routing

Detail is a **stack screen nested within each tab**: `src/app/(tabs)/discover/detail.tsx` and `src/app/(tabs)/library/detail.tsx` both render the same `DetailScreen` component. Each tab has its own Stack layout, enabling unlimited navigation depth (discover → artist → album → track → ...) with natural back-button behavior. The component uses `useSegments()` to determine which tab it's in and build correct push paths. Tapping the tab bar icon resets the stack to the tab root.

## Dependencies

- `@shared/lib/detail-handoff` — the discover↔detail seam.
- `@shared/api-client/tracks` (`createTrack`) + `types` (`CreateTrackRequest`, `TrackResponse`).
- `@shared/api-client/discovery` (`DiscoveryResult`, `getAlbumTracks`, `getArtistTopTracks`, `getArtistAlbums`, `ContentFetchResponse`).
- `@tanstack/react-query` — `useSaveTrack` mutation, via the root `QueryClientProvider`.
- `@shared/ui/primitives/*` — `Screen`, `Text`, `Artwork`, `Button`, `Banner`.
- No cross-feature imports (vertical-slice rule).

## Test files

- `__tests__/extras.test.ts` — `trackInfoRows` / `formatDuration`.
- `__tests__/save-cache.test.ts` — optimistic cache transforms + request mapping.
- `__tests__/useSaveTrack.test.ts` — optimistic insert + rollback against a real QueryClient.
- `__tests__/useLateralNav.test.ts` — search-and-navigate for lateral browsing.
- `__tests__/DetailScreen.test.tsx` — header, redirect, per-kind bodies, Save press + disable, lateral navigation links, catalog browse mocks.

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
- **Lateral navigation via search** — `useLateralNav` hook searches for artist/album by name (`searchDiscovery` with `limit: 1`) and navigates via `router.push` within the current tab's stack (uses `useSegments` to determine `/discover/detail` or `/library/detail`). Artist name is tappable on track/album detail; album row is tappable on track detail when `extras['album']` exists. Shows Banner on failure (not Alert); loading indicator while searching.
- **Album content fetch** — `useAlbumTracks` hook calls `getAlbumTracks(provider, externalId)` using the first source from the album's `sources[]`. React Query cached per `(provider, external_id)` with 30min staleTime. Track rows are tappable → navigate to track detail.
- **Artist content fetch (multi-provider + SoundCloud)** — `useArtistContent` hook accepts the full `sources` array + `artistName`. `bestSourcePerProvider` picks ONE source per provider by priority (deezer > lastfm > musicbrainz), shared between tracks and albums — prevents querying wrong same-name artists when the merged search result carries multiple sources from one provider. Albums fan out to these sources in parallel via `Promise.allSettled`, PLUS a separate SoundCloud query using the artist name (the SC adapter resolves the name to a username internally). All results merged/deduped by `normalizeForDedup` (strips bracketed suffixes like "(Deluxe)", lowercases, collapses whitespace — catches "A Great Chaos" vs "A Great Chaos (Deluxe)"). Keep highest `track_count`. SC sets with no artwork are back-filled from title-matched albums from other providers before dedup. Albums sorted by `release_date` or `year` descending.
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

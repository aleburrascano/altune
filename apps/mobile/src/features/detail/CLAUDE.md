# detail — feature-local context

Read-only detail screen for a tapped discovery result (`view-result-detail` spec). Fed by an in-memory handoff — no per-item backend fetch. A track can be saved to the library with an optimistic UI. Album detail shows tracklist fetched from provider API; artist detail shows top tracks + albums. Lateral navigation (AC#11-13) allows tapping artist/album names to browse related content; content items are tappable to navigate deeper (AC#14-20).

## Key terms

- **Handoff** — the last-tapped `DiscoveryResult`, stashed in `@shared/lib/detail-handoff` (shared, not feature-local: discover writes, detail reads). DetailScreen reads it on mount; an empty handoff (cold start / reload / deep link) redirects to `/discover`.
- **AcquisitionStatus pending** — a freshly saved track starts `pending` (audio not yet acquired); the library row shows a "Pending" marker.

## Patterns specific here

- **Pure helpers, thin JSX** (same as discover/library). `extras.ts` (`trackInfoRows`, `formatDuration`) and `save-cache.ts` (`insertOptimisticTrack`, `optimisticTrack`, `toCreateTrackRequest`) hold the logic and are unit-tested without rendering; `DetailScreen.tsx` is the wrapper.
- **Primitives imported directly** (`@shared/ui/primitives/*`), not the barrel, to keep jest transitive loads small. `Artwork` → `expo-image` is mocked in the component test.
- **Optimistic save.** `useSaveTrack` prepends a pending placeholder to the `['library']` infinite-query cache on mutate, rolls back to the snapshot on error, and invalidates on settle so a dedup hit reconciles to the server row. Cache transforms are pure (`save-cache.ts`). The optimistic placeholder includes null values for the extended metadata fields (`year`, `genre`, `track_number`, `album_artist`, `isrc`, `audio_ref`) added by the `import-legacy-library` spec.
- **`extras` is an untyped wire map.** Every key is narrowed before use; absent/empty values are omitted. Track detail shows only `duration_seconds` and `album` (ISRC and popularity removed — not user-facing per Spotify parity). `formatDuration` promoted to `@shared/lib/format` (2-consumer rule); re-exported from `extras.ts` for backwards compatibility.
- **Save guarded by the Track artist invariant** — when `result.subtitle` (artist) is null the Save button is disabled and `onSave` short-circuits (no invalid POST).
- **Lateral navigation via search** — `useLateralNav` hook searches for artist/album by name (`searchDiscovery` with `limit: 1`) and navigates via `router.replace('/detail')` (not push) to keep the back stack shallow. Artist name is tappable on track/album detail; album row is tappable on track detail when `extras['album']` exists. Shows Banner on failure (not Alert); loading indicator while searching.
- **Album content fetch** — `useAlbumTracks` hook calls `getAlbumTracks(provider, externalId)` using the first source from the album's `sources[]`. React Query cached per `(provider, external_id)` with 30min staleTime. Track rows are tappable → navigate to track detail.
- **Artist content fetch** — `useArtistContent` hook fetches top tracks (limit 5) and albums (limit 10) in parallel. **Provider priority**: Deezer > Last.fm > MusicBrainz (Deezer/Last.fm have popularity data; MB returns arbitrary recordings). Albums sorted by `release_date` or `year` (MB uses year, Deezer uses release_date) descending.
- **Single-scroll layout** — album and artist detail wrap hero + content in one `ScrollView` (no nested scroll). Track detail has no scroll (content is short). This prevents the nested-scroll UX antipattern where the user has to scroll in a small area.
- **Back button safety** — `router.canGoBack()` checked before `router.back()`; if false, navigates to `/discover`. Prevents GO_BACK error when detail is the root.
- **Accessibility** — all tappable elements have `accessibilityRole` + `accessibilityLabel`: back button, artist/album links, track rows, album cards. Touch targets ≥48pt.

## TestIDs (load-bearing)

**Track detail:** `detail-header`, `detail-back`, `detail-artist-link` (tappable artist name), `detail-track-info`, `detail-info-<key>` (duration/album/isrc/popularity — album is tappable), `detail-save`, `detail-save-error`.

**Album detail:** `detail-tracklist-loading`, `detail-tracklist-error`, `detail-tracklist-empty`, `detail-tracklist` (success), `detail-track-<n>` (each track row, 0-indexed).

**Artist detail:** `detail-artist-content` (container), `detail-top-tracks-loading`, `detail-top-tracks-error`, `detail-top-track-<n>` (each top track), `detail-albums-loading`, `detail-albums-error`, `detail-album-<n>` (each album card).

## Routing

`src/app/(tabs)/detail.tsx` is **inside** the `(tabs)` group with `href: null` so the tab bar remains visible but detail doesn't appear as a tab button. Navigation: `DiscoverScreen.onResultTap` calls `stashHandoffForDetail(result)` then `router.push('/detail')`; the click-tracking mutation stays fire-and-forget.

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

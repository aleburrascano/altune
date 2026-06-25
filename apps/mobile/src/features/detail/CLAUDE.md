# detail — feature-local context

Read-only detail screen for a tapped discovery result (`view-result-detail` spec). Fed by an in-memory handoff — no per-item backend fetch. A track can be saved to the library with an optimistic UI and a visible acquire lifecycle. Album detail shows a tracklist fetched from provider API; artist detail shows top tracks + discography. Lateral navigation (AC#11-13) allows tapping artist/album names to browse related content; content items are tappable to navigate deeper (AC#14-20).

The screen was **reworked to declutter**: one vertical scroll of **header → per-kind body → optional `Disclosure`**, with no tabs and no always-on provider slabs (see "Screen shape" below).

## Key terms

- **Handoff** — the last-tapped `DiscoveryResult`, stashed in `@shared/lib/detail-handoff` (shared, not feature-local: discover writes, detail reads). DetailScreen reads it on mount; an empty handoff (cold start / reload / deep link) redirects to `/discover`.
- **Save / acquire lifecycle** — a freshly saved track starts `pending` (audio acquiring server-side), then reconciles to `ready` or `failed`. On the detail screen the `TrackSaveControl` renders this as add → saving (spinner) → ready (check) → failed (retry); the state is derived from the library React Query cache by the pure `saveControlState` helper (`save-control-state.ts`). It advances on query invalidation today; the backend `/v1/events` SSE stream (`track_acquired` event) makes it instant.

## Screen shape (post-rework)

One scroll: **header → per-kind body → optional `Disclosure`**.

- **Header** (`DetailScreen.tsx`) — back · hero artwork (square track/album, circular artist) · title · an `artist · year` subtitle (artist is a tappable lateral-nav link; the year is appended muted). No kind label.
- **Genres** — one identity pill row (`GenrePills`, MusicBrainz genres ≤4) at the top of each body, replacing the four per-provider genre/tag blocks.
- **Deep metadata** — lives behind a single collapsed `Disclosure`, mounted only when its enrichment is present: album → `DiscogsEnrichmentSection` + `DeezerEnrichmentSection` under "Details & credits" (`detail-album-details`); artist → `LastFmEnrichmentSection` + `DiscogsArtistSection` under "About &lt;artist&gt;" (`detail-artist-about`). **Lyrics are no longer on the detail screen** (they belong to the full player) — `LyricsSection` still exists as a component but is unrendered here. The MusicBrainz `EnrichmentSection` is also no longer mounted (genres/year surface via `GenrePills` + the header).

## Patterns specific here

- **Pure helpers, thin JSX** (same as discover/library). `extras.ts` (`trackInfoRows`, `formatDuration`) and `save-cache.ts` (`insertOptimisticTrack`, `optimisticTrack`, `toCreateTrackRequest`) hold the logic and are unit-tested without rendering; `DetailScreen.tsx` is the wrapper.
- **Primitives imported directly** (`@shared/ui/primitives/*`), not the barrel, to keep jest transitive loads small. `Artwork` → `expo-image` is mocked in the component test.
- **Optimistic save.** `useSaveTrack` prepends a pending placeholder to the `['library']` infinite-query cache on mutate, rolls back to the snapshot on error, and invalidates on settle so a dedup hit reconciles to the server row. Cache transforms are pure (`save-cache.ts`). The optimistic placeholder includes null values for the extended metadata fields (`year`, `genre`, `track_number`, `album_artist`, `isrc`, `audio_ref`) added by the `import-legacy-library` spec.
- **`extras` is an untyped wire map.** Every key is narrowed before use (`extras-accessors.ts`); absent/empty values are omitted. Post-rework the track body shows **duration on the Play button** (`Play · 4:03`), and **album + featured artists as tappable nav rows** (`detail-info-album` / `detail-info-featuring`) — not a per-field info-row wall. Genres come from MusicBrainz (`GenrePills`, ≤4); the year is shown in the header `artist · year` subtitle (MusicBrainz year, else extras/album year). Featured artists come from MB's `artist-credit` array (structured) OR regex parsing of "feat./ft./with" in title/subtitle (fallback for Deezer/iTunes/SC). `trackInfoRows` in `extras.ts` is now **unused by the screen** (kept for its unit test). `formatDuration` lives in `@shared/lib/format`, re-exported from `extras.ts`.
- **`TrackSaveControl`** — the 40pt circular save/download control (`ui/TrackSaveControl.tsx`) shared by the album tracklist rows (`AlbumTrackRow`) and the artist Popular-Tracks rows. State comes from each kind's state hook: `useAlbumDetailState.saveStateFor` / `useArtistDetailState.saveStateFor` (and `onQuickSave`), both reading the library cache. Replaces the old bare 16pt Plus; clears the 44pt touch-target bar (40pt circle + `hitSlop`).
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

**Header / shared:** `detail-header`, `detail-back`, `detail-artist-link` (tappable artist name), `detail-genres` (genre pill row).

**Track detail:** `detail-track-info` (body), `detail-play` / `detail-preview` (play/preview Button), `detail-save`, `detail-save-error`, `detail-info-album` (tappable album nav row), `detail-info-featuring` (featured-artist nav row), `detail-lateral-error`. **Removed by the rework:** `detail-info-duration`, `detail-info-isrc`, `detail-info-popularity`, and the on-screen lyrics testIDs.

**Album detail:** `detail-tracklist-loading`, `detail-tracklist-error`, `detail-tracklist-empty`, `detail-tracklist` (success), `detail-track-<n>` (each track row, 0-indexed), `detail-track-save-<n>` (per-row save control), `detail-album-meta` (year · tracks · duration), `detail-album-details` (Details &amp; credits Disclosure).

**Artist detail:** `detail-artist-content` (container), `detail-top-tracks-loading`, `detail-top-tracks-error`, `detail-top-track-<n>` (each top track), `detail-top-track-save-<n>` (per-row save control), `detail-albums-loading`, `detail-albums-error`, `detail-album-<n>` / `detail-single-<n>` / `detail-ep-<n>` (discography cards), `detail-artist-about` (About Disclosure).

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
- `__tests__/save-control-state.test.ts` — the add/saving/ready/failed lifecycle mapping (pure).
- `__tests__/save-cache.test.ts` — optimistic cache transforms + request mapping.
- `__tests__/useSaveTrack.test.ts` — optimistic insert + rollback against a real QueryClient.
- `__tests__/useLateralNav.test.ts` — search-and-navigate for lateral browsing.
- `__tests__/DetailScreen.test.tsx` — header, redirect, per-kind bodies, Save press + disable, lateral navigation links, catalog browse mocks.

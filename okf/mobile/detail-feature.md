---
type: Mobile Feature
title: Detail
description: Read-only track/album/artist detail screen fed by an in-memory handoff, with multi-provider enrichment and optimistic save-to-library.
resource: apps/mobile/src/features/detail/
tags: [mobile, feature, detail, enrichment, react-query, optimistic-update]
verified_commit: 6a047a008fb23b38e719d9a9a3e9b539ab349d4d
---

Renders the detail screen for a tapped discovery result. It is fed entirely by an in-memory handoff (`@shared/lib/detail-handoff`, written by [[discover-feature]]) — there is no per-item backend fetch on open; an empty handoff (cold start/reload/deep link) redirects to `/discover`. `ui/DetailScreen.tsx` is one vertical scroll: sticky back button → hero (artwork + title + tappable `artist · year` subtitle) → a per-kind body (`TrackDetailBody`/`AlbumDetailBody`/`ArtistDetailBody`) → an optional collapsed `Disclosure` for deep provider metadata. There are no tabs and no always-on provider slabs (post-rework simplification).

**Enrichment**: `hooks/useDetailEnrichments.ts` is the single seam deciding which of six provider hooks (MusicBrainz `useEnrichment`, Deezer, Last.fm, Discogs album/artist, Deezer lyrics) fire for a given `kind`, expressed via each hook's `enabled` flag rather than conditional hook calls (React's rules of hooks). Every provider hook is built on the shared `useEnrichmentQuery` skeleton (Template Method, function-value form) — one `useQuery` with a 24h `staleTime`, gated by a `hasContent` predicate so an unresolved/empty payload collapses to `null` and its section hides. This mirrors the backend's own [[enrichment]] subsystem, one hook per provider surface. MusicBrainz supplies the identity genre pills and HD artwork; Discogs/Last.fm/Deezer sit behind the `Disclosure` ("Details & credits" for album, "About <artist>" for artist).

**Save-to-library**: `hooks/useSaveTrack.ts` is a TanStack Query mutation with a full optimistic-update lifecycle — `onMutate` prepends a pending placeholder to the `['library']` infinite-query cache (pure transforms in `save-cache.ts`), `onError` rolls back to the pre-mutation snapshot, `onSettled` invalidates so a dedup hit reconciles. `TrackSaveControl` renders the add → saving → ready/failed states, derived from the library cache by `save-control-state.ts`. On success it also enqueues a `library_add` telemetry event via the critical outbox (see [[shared-telemetry]]), carrying the originating `search_id` for the behavioral corpus.

**Lateral navigation**: `useLateralNav` searches by name (`limit:1, saveHistory:false`) and pushes a fresh detail screen for tapped artist/album/featured-artist names, guarded against duplicate pushes. Album/artist content (`useAlbumTracks`, `useArtistContent`) fetches from provider APIs directly (not the handoff), merging MusicBrainz and Deezer sources with an MB-authoritative filter when identity is verified.

Key files: `hooks/useDetailEnrichments.ts`, `hooks/useEnrichmentQuery.ts`, `hooks/useSaveTrack.ts`, `ui/DetailScreen.tsx`, `ui/TrackDetailBody.tsx`, `ui/ArtistDetailBody.tsx`, `ui/AlbumDetailBody.tsx`, `save-cache.ts`, `save-control-state.ts`.

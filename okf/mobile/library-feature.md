---
type: Mobile Feature
title: Library
description: Single chip-filtered personal-collection screen (Playlists/Songs/Albums/Artists) with client-side grouping and acquisition retry.
resource: apps/mobile/src/features/library/
tags: [mobile, feature, library, playlists, react-query, grouping]
verified_commit: 6a047a008fb23b38e719d9a9a3e9b539ab349d4d
---

The user's personal collection screen (`docs/superpowers/specs/2026-06-28-library-redesign-design.md`). Model: **spine + lenses** — Songs and Playlists are what the user owns (primary, fetched/mutated), while Albums and Artists are client-derived groupings of saved songs (lenses, no backend endpoint). `ui/LibraryScreen.tsx` is the orchestrator: it owns the active `LibraryChip` (`'playlists'|'songs'|'albums'|'artists'`, opens on Playlists), a per-chip sort key, search, and the track action sheet, and renders header → search → chip bar → sort control → the one active view — no stacked overview, selecting a chip swaps only the content area.

**Data**: `hooks/useLibraryHome.ts` fetches all tracks in one page (`limit: 2000`) and polls every 5s while any track's `acquisition_status` is `pending`, so a freshly-saved track's save→ready/failed lifecycle (see [[acquisition]]) resolves without a manual refresh. `hooks/useLibraryGrouping.ts` derives `AlbumGroup`/`ArtistGroup` from that flat track list via `@shared/lib/derive-library-groups` — pure client-side grouping, not domain types. `state.ts`'s `_viewForState` is the same pure loading > error > empty > list precedence pattern used across features. `hooks/useRetryAcquisition.ts` wraps the retry-acquisition mutation and invalidates `library-home`/`library`/`playlist`/`playlists` caches on success so a retried track reconciles everywhere it's rendered. Note: `hooks/useLibrary.ts` today contains only pagination helpers (`_nextOffsetFromPage`/`_flattenPages`) — the live `useInfiniteQuery` hook it once exported was retired and has no current caller; only its own test file still uses the pure helpers.

**Playlist detail**: `ui/PlaylistDetailScreen.tsx` (route `/library/playlist/[id]`) fetches one playlist by id and layers optimistic mutations on it — rename and remove-track use full `onMutate`/rollback optimism (snapshot the name / filter the cached track list), while delete is a plain mutate-then-navigate-back (not optimistic) — each with an `Alert.alert` failure surface. Play/shuffle build a playable queue via `buildPlayableQueue` and hand it to `useQueuePlayback` (see [[shared-playback]]).

**Navigation**: tracks/albums/artists build a `DiscoveryResult` shape and navigate to `/library/detail` through `useLibraryNavigation`, reusing the same detail-handoff seam `discover` uses — the `detail` feature is kind-agnostic about which tab launched it.

Key files: `hooks/useLibraryHome.ts`, `hooks/useLibraryGrouping.ts`, `state.ts`, `ui/LibraryScreen.tsx`, `ui/PlaylistDetailScreen.tsx`, `hooks/useRetryAcquisition.ts`.

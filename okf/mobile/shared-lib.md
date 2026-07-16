---
type: Subsystem
title: Shared lib (utilities)
description: A deliberately small grab-bag of pure utilities promoted here only once a second feature needed them, including the in-memory discover-to-detail handoff seam.
resource: apps/mobile/src/shared/lib/
tags: [mobile, shared, utilities]
verified_commit: e238cc3671d1719837686c667242c7d88fc376d2
---

Everything here is a pure module promoted on its second consumer. `async-view.ts` — `asyncView` encodes the one shared precedence rule *loading > error > empty > ready*; the [library](library-feature.md) and [discover](discover-feature.md) `state.ts` each wrap it with their own definition of what counts as loading/empty. `derive-library-groups.ts` — `deriveAlbums`/`deriveArtists` fold a `TrackResponse[]` into `AlbumGroup`/`ArtistGroup` (UI read-side lenses, not domain types) via one shared `deriveGroups` fold (track count, most-recent `added_at`, first-available artwork); used by library grouping and the [detail](detail-feature.md) artist screen. `featured.ts` — `featuredArtistsFromExtras` parses the `featured_artists` extras key (tolerating legacy bare-string names as id-less credits) and `withFeaturing` renders the comma-joined co-billed credit ("Ken Carson, Playboi Carti"); used by library/discover rows, detail, and all three playback player surfaces. `format.ts` — `formatDuration` (m:ss); library rows and detail extras. `track-to-discovery.ts` — `trackToDiscoveryResult` adapts a saved library `TrackResponse` into the discovery wire shape so a saved Track flows through the same handoff path as a search result; used by library navigation and detail's album/artist/lateral navigation.

The one real invariant lives in `detail-handoff.ts`: it is the **in-memory seam between discover and detail**. Discover (and library, via `trackToDiscoveryResult`) stashes the tapped `DiscoveryResult` plus its originating `search_id` (so downstream engagement telemetry — library_add, play — can join back to the search) and navigates; `DetailScreen` reads it on mount instead of doing a per-item backend fetch. A cold start (deep link, reload) leaves the handoff null and detail redirects to `/discover`. It sits in shared/ because it has a writer (discover, library) and a reader (detail) in different features — it cannot be a cross-feature import.

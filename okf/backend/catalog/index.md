---
type: Index
title: Catalog subsystems
description: The catalog bounded context decomposed into its four sub-features — Track aggregate/dedup, Playlist aggregate, featured-artist credits, and audio storage/streaming.
tags: [index, catalog]
verified_commit: b1b3e3867ff5d3319beb9b3d361d8625cea3ec94
---

Catalog is the identity-and-metadata bounded context for a user's saved music (`services/go-api/internal/catalog/`). Its two aggregate roots are `Track` and `Playlist`, both identified by wrapped-UUID value objects and owned by a `shared.UserId`. Every coded error in the context (`ValidationError`, `ErrTrackAlreadyInPlaylist`, the `service.Err*` sentinels) implements `HTTPStatus()`, so handlers route uniformly through `httputil.HandleServiceError`.

- [track](track.md) — the `Track` aggregate, its acquisition-status invariant, dedup-key upsert; `AddTrackService`, `DeleteTrackService`, `SetTrackNumberService`
- [playlist](playlist.md) — the `Playlist` aggregate, contiguous-position invariant; `PlaylistLifecycleService`, `PlaylistMembershipService`
- [featured-artists](featured-artists.md) — the `FeaturedArtist` value object, identity-key mirroring with the `featured_artists` table; `BackfillFeaturedService`, `ListFeaturingService`
- [audio-storage](audio-storage.md) — `AudioStore`/`AudioURLSigner` ports, filesystem and object-storage adapters, `StreamTrackService`, presigned-URL issuance

Acquisition is reached only through the `AcquisitionScheduler` port, injected into `AddTrackService` and `StreamTrackService` — catalog never imports the acquisition context. Featured-artist resolution similarly reaches discovery only through `adapters/discoverybridge`, never a direct import (see [featured-artists](featured-artists.md)).

## Library lenses (server-owned grouping, search and sort)

Albums and Artists used to be **client-derived**: the mobile app paged the whole track list onto the device and folded it into groups in JS, then filtered and sorted it there too. That made the phone hold the entire library just to answer questions Postgres answers better, and it made the derived lenses silently wrong whenever the fetch truncated.

They are now read models produced by SQL. `domain/library_lens.go` holds the value types (`AlbumGroup`, `ArtistGroup`), the query (`LibraryQuery`) and the sort enum (`LibrarySort`: `recent` / `az` / `year`, parsed at the edge and rejected loudly rather than defaulted). `adapters/persistence/library_lens_repo.go` groups by `lower(album) || '|||' || lower(coalesce(album_artist, artist))` for albums and `lower(artist)` for artists — the same key the client used, so nothing regroups — picking the most recently added track's artwork and year per group via `array_agg(... ORDER BY added_at DESC) FILTER (WHERE ... IS NOT NULL)`. `LibraryLensService` is the use case; `adapters/handler/library_handler.go` serves `GET /v1/library/albums` and `GET /v1/library/artists`, both taking `?q=` and `?sort=`.

`sort=year` is rejected for artists (`ValidationError`, 400) rather than quietly falling back — a track has a year, an artist does not, and a silent substitution would look like a broken sort.

`GET /v1/tracks` gained the same `?q=` and `?sort=`, applied in SQL by `ListFilteredForUser`. `ListTracksService.Execute` now takes a `LibraryQuery` instead of a bare limit/offset. The old unfiltered `ListForUser` survives for `BackfillFeaturedService`, which pages the whole library deliberately.

`ListOwnedTrackRefs` is a narrow projection — id, title, artist, acquisition status, track number — used by discovery's ownership stamping (see [discovery](../discovery/index.md)); it deliberately skips the featured-artists join, like the other narrow reads.

## Failure copy and playlist duration

`domain.FailureMessage` maps an acquisition failure reason (`no_match_found`, `download_failed`, `ytdlp_error`, or nothing) to the sentence a user reads; it surfaces as `failure_message` on `TrackDTO` whenever the track is failed. The client used to own that map, which meant a new backend reason silently rendered as generic copy on an app that had not shipped yet.

`domain.TotalDurationSeconds` sums a playlist's tracks into `total_duration_seconds` on the playlist detail response.

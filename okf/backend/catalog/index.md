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

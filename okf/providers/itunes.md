---
type: Provider Integration
title: iTunes Search API Adapter
description: The keyless iTunes Search API supplies a mainstream search/identity surface and — its one non-duplicative axis — a 1500px hero-artwork fallback above Cover Art Archive's ceiling.
resource: services/go-api/internal/discovery/adapters/providers/itunes.go
tags: [discovery, provider, itunes, artwork]
verified_commit: 6a047a008fb23b38e719d9a9a3e9b539ab349d4d
---

`ITunesAdapter` (`itunes.go`) calls `itunes.apple.com/search` and `/lookup` with no key, token, or required User-Agent (a custom UA — `itunesUserAgent` — is still sent to soften Apple's abuse heuristics on the default Go UA). `rateLimit` implements a GCRA token bucket (`itunesEmitInterval = 4s` sustained ~15 req/min, `itunesBurst = 4`) rather than a fixed-gap sleep, so a single search's 3 sequential kind calls fire back-to-back instead of being spaced ~3.5s apart — the old fixed-gap limiter blew the search SLA. `SearchTimeout` is 4s. Unconditionally wired (no config gate, like Deezer).

`Search` covers track/album/artist (`searchKind`, `itunesEntity`) at `limit=200` (the API max) for deeper merge recall at no extra rate cost. `Resolve` implements `ArtworkResolver` via `upscaleArtwork`, a URL rewrite of the `100x100bb` template: search-card thumbnails use `iTunesListArtworkSize = 600` (applied in `mapITunesResult`), and the detail-open hero uses `iTunesHeroArtworkSize = 1500` (the value `Resolve` itself rewrites to) — comfortably above Cover Art Archive's 1200px ceiling (Apple's real master resolves to ≥3000px, live-verified), at a fraction of the ~2.4MB a full 3000px hero would cost. Wired after the MBID-keyed sources (CAA, Fanart, Deezer, etc.) in `buildArtworkChain` (see [artwork-chain](artwork-chain.md)), so it only fires on their miss. `LookupAlbum` feeds the album-contamination consensus check (`AlbumVerdict` + `artistId`) via name + genre-cluster overlap (`stripITunesTypeSuffix` strips `" - Single"`/`" - EP"`/etc. before comparing).

`GetAlbumTracks`/`GetArtistTopTracks`/`GetArtistAlbums` implement `Album`/`ArtistContentProvider` via `/lookup` (`lookupContent`), filtering to the requested child `wrapperType` (`itunesContentTarget`) to drop the parent wrapper uniformly — a second mainstream discography source alongside Deezer, though iTunes tracks are catalog-ordered (recent-first), not popularity-ranked, unlike Deezer's `/artist/{id}/top`. `itunesSourceRef` gives each kind its own id (`collectionId`/`artistId`/`trackId`) rather than always `trackId`, fixing a prior bug where album/artist `SourceRef`s carried an unusable `"0"`.

Caveats: no ISRC/MBID — artwork/album resolution is a name-match risk, mitigated by running iTunes last in the chain; artist entities carry no artwork; the ~20 req/min rate ceiling is `[INFERRED]`, not header-exposed. Code matches `docs/providers/itunes.md` — no material drift found.

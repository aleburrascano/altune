---
type: Provider Integration
title: Deezer Adapter
description: Deezer's public API and internal pipe GraphQL power search, artwork fallback, charts, content, ISRC lookup, detail-open metadata (bpm/gain/genres/label), and time-synced lyrics.
resource: services/go-api/internal/discovery/adapters/providers/deezer.go, services/go-api/internal/discovery/adapters/providers/deezer_enrichment.go, services/go-api/internal/discovery/adapters/providers/deezer_lyrics.go
tags: [discovery, provider, deezer, lyrics, artwork]
verified_commit: 6a047a008fb23b38e719d9a9a3e9b539ab349d4d
---

Deezer is altune's popularity primary (`nb_fan`/`rank`) and the only wired provider carrying lyrics. Two access tiers: the public `api.deezer.com` (no auth, no key — every call in `deezer.go` fires a bare `GET`) and the internal `pipe.deezer.com` GraphQL, reached via an anonymous JWT bootstrapped from `auth.deezer.com/login/anonymous` (reverse-engineered, against ToS, accepted for self-hosted personal/family use per the README doctrine).

`DeezerAdapter` (`deezer.go`) implements `Search`/`SearchStructured` across track/album/artist (`searchKind`, `deezerSearchEndpoint`), `Resolve` (`ArtworkResolver`, preferring 1000px `cover_xl`/`picture_xl` over the 500px `_big` fallback, skipping `IsDeezerPlaceholder`), `GetAlbumTracks`/`GetArtistTopTracks`/`GetArtistAlbums` (`Album`/`ArtistContentProvider`), `FetchCharts` (`ChartProvider`), and `FetchTrackISRC`/`FetchFirstTrackID` feeding the identity/consensus engine (see [[merge-dedup]]). Unconditionally wired in `app.go` — no config gate, since the public API needs no key.

The same struct also implements `ports.DeezerEnricher` (`deezer_enrichment.go`): `ResolveID` maps `(kind, artist, title)` to a Deezer id via structured search; `Lookup` fetches `/track/{id}` (bpm rounded, gain, `explicit_lyrics`) or `/album/{id}` (label, genres capped at `deezerGenresCap = 6`, record_type, UPC). Display-only, off the ranking path (see [[enrichment]]).

`DeezerLyricsAdapter` (`deezer_lyrics.go`) implements `ports.LyricsProvider`: `ResolveTrackID` delegates to the public-API adapter's search; `Lookup` POSTs the `SynchronizedLyrics` query to `pipe.deezer.com`, gated by `deezerJWTResolver` — a singleflight-deduped, self-healing anonymous-JWT cache that re-bootstraps once on `401`. A null `lyrics` object or GraphQL error array is a definitive miss (empty + nil, negative-cacheable), never a failure.

Caveats: rate limit ~50 req/5s per IP is `[INFERRED]`, unconfirmed via headers; `bpm` is sparse (render only when `> 0`); lyrics availability is region/catalog-dependent; the JWT response field name (`jwt`) is explicitly `[INFERRED]` in the code comment, unverified against a live-dumped auth response. Code matches `docs/providers/deezer.md` closely — no material drift found.

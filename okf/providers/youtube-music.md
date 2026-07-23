---
type: Provider Integration
title: YouTube Music
description: Keyless internal-API adapter for track/video/album/artist search plus a self-owned response parser, whose one non-duplicative contribution is hi-res artist artwork.
resource: services/go-api/internal/discovery/adapters/providers/ytmusic.go, services/go-api/internal/discovery/adapters/providers/ytmusic_client.go, services/go-api/internal/discovery/adapters/providers/search_kinds.go
tags: [discovery, provider, youtube-music, artwork, search]
verified_commit: b1b3e3867ff5d3319beb9b3d361d8625cea3ec94
---

`YouTubeMusicAdapter` (`ytmusic.go`) searches YouTube Music's internal `music.youtube.com/youtubei/v1/search` endpoint — the same backend the WEB_REMIX web player calls. `Search` dispatches one unfiltered query per call and maps `result.Tracks`, `result.Videos` (obscure/UGC recordings YT Music files as videos — mapped to tracks so they aren't lost from the candidate set, per an `AIDEV-NOTE` in the file), `result.Albums`, and `result.Artists`. `GetArtistAlbums`/`GetArtistTopTracks` implement `ports.ArtistContentProvider`, filtering results to an exact case-insensitive artist-name match. The parser lifts the explicit badge into `ytmTrack.IsExplicit`/`ytmAlbum.IsExplicit` (`ytmHasExplicitBadge`); `mapYTMusicTrack`/`mapYTMusicAlbum` now carry it into `extras["explicit"]` (previously parsed then dropped), alongside the album `year` and `record_type` they already map.

`mapYTMusicTrack` and `mapYTMusicVideo` now set `domain.SearchResult.Duration` (from `t.Duration`/`v.Duration` on the parsed `ytmTrack`/`ytmVideo` structs) after constructing the result via `NewProviderResult`; `mapYTMusicTrack` additionally sets `.Album` from `t.Album.Name`. Previously `NewProviderResult`'s return value was returned directly, so these fields — already present on the parsed structs — were never carried onto the `SearchResult`.

**Auth model.** Gated only by a public innertube key (`ytmSearchKey`, hardcoded in `ytmusic_client.go`) shipped in the web player's JS — no user auth, no quota. Stable since first observed; if it rotates, the fix is the same shape as SoundCloud's `client_id` scrape (see [soundcloud](soundcloud.md)) — not yet implemented, not yet needed.

**Rate limits.** Not header-exposed. Bursty/concurrent calls trip an intermittent HTTP 403 whose body is HTML (surfaces as a JSON decode error). `ytmSearchRetry` (`ytmusic.go`) retries once with a 250ms backoff, respecting `ctx.Done()`. `SearchTimeout()` returns 3s; the underlying HTTP client (`ytmHTTPClient`) is bounded at `ytmusicTimeout = 8s`.

**Key artifact — the self-owned parser.** `ytmusic_client.go` is a from-scratch YouTube Music request + response parser (`ytmSearch`, `parseYTMSearch`) that explicitly replaces `github.com/raitonoberu/ytmusic`: the file's header comment states the third-party library's parser silently returned zero results after YouTube restructured its search response (moving from a single `musicShelfRenderer` to a `musicCardShelfRenderer` "top result" card plus `itemSectionRenderer` sections). The request shape (endpoint, `WEB_REMIX` context, key) is unchanged from the library; only the response parsing was rebuilt, keyed by per-item `pageType`/`musicVideoType` rather than by container name.

**Artist artwork (the headline capability).** `YouTubeMusicArtworkResolver.Resolve` (artist-only) is the one non-duplicative metadata win: iTunes (the best keyless artwork source) has no artist images, Fanart.tv is MBID-gated, and the official Data-API resolver is quota-crippled. `pickArtistArtwork` prefers an exact name match, falls back to the top hit, and `resizeYTThumbnail` rewrites the Google-hosted thumbnail's `w{N}-h{N}` segment to a 1000px hero. Wired late in the artwork chain (after ID-keyed sources, before SoundCloud — see [artwork-chain](artwork-chain.md)).

**`search_kinds.go` note.** `searchAcrossKinds`/`defaultKindOrder` is a shared sequential per-kind fan-out helper used by other kind-iterating providers that must serialize calls to respect a host's rate limit (Deezer, iTunes, Last.fm, MusicBrainz). YouTube Music does **not** use it — it issues one unfiltered search per query and gets all kinds back in a single response, so this file is infrastructure shared across other providers, not YT-Music-specific.

**Doc drift (`docs/providers/youtube-music.md`).** This is the significant one: the doc repeatedly describes the adapter as built on `github.com/raitonoberu/ytmusic`. The code has since replaced that library's parser entirely with the self-owned `ytmusic_client.go`, precisely because the library broke. The doc's endpoint catalog, auth model, entity model, and artist-artwork capability description otherwise still hold — only the "which code parses the response" claim is stale.

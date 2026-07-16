---
type: Provider Integration
title: Genius
description: Config-gated, identity-blind artwork resolver that name-searches Genius's song API for song/artist images — it carries no lyrics functionality despite the file name.
resource: services/go-api/internal/discovery/adapters/providers/genius.go
tags: [discovery, provider, genius, artwork]
verified_commit: 6a047a008fb23b38e719d9a9a3e9b539ab349d4d
---

Despite living in `genius.go` and the ubiquitous-language glossary's `LyricsProvider`/`DeezerLyrics` being the primary lyrics surface (see [deezer](deezer.md)), **Genius plays no lyrics role in this codebase today.** The file exports exactly one type, `GeniusArtworkResolver`, implementing `ports.ArtworkResolver` (`Resolve`) plus `ResolveWithHints` and `ArtworkSource() string { return "genius" }` (`ports.SourcedArtworkResolver`). There is no lyrics fetch, no lyrics type, no lyrics port satisfied anywhere in the file — it is purely an image resolver that happens to use Genius's song-search API as its data source.

**What it contributes.** `resolveSongImage` (track/album path) queries `GET https://api.genius.com/search?q=<artist title>` and pulls `song_art_image_url` (falling back to `header_image_url`) off the top hits, rejecting anything containing `"default"` or `"no_image"`. `resolveArtistImage` runs up to `1 + 1 + maxHintSearches(3)` queries — the bare artist name, `"{artist} songs"`, and up to 3 `"{artist} {trackHint}"` combinations — and `findArtistImageInHits` picks the `primary_artist.image_url` only when the hit's artist name case-insensitively matches the query artist exactly, guarding against a same-name artist inheriting the wrong face.

**Auth model.** Bearer-token auth: every `searchGenius` call sends `Authorization: Bearer {accessToken}` via the shared `getJSON(... withHeader(...))` helper. The token comes from `GeniusAccessToken` (env `GENIUS_ACCESS_TOKEN`), and the resolver is only constructed when `cfg.HasGenius()` is true — config-gated like Discogs and Fanart.tv, unlike the always-on keyless providers (Deezer, iTunes, SoundCloud, YouTube Music).

**Rate limits.** None enforced in the adapter itself — no retry, no backoff, no rate limiter visible in `genius.go`. Errors from `searchGenius` are swallowed (`return "", nil`) at every call site so a Genius failure never blocks the artwork chain; it just falls through to the next resolver.

**Why it's excluded from search.** Per `services/go-api/internal/discovery/ARCHITECTURE.md`, Genius is marked "❌ song/lyrics shape" for catalog search: its hits mix song+artist information in a way that doesn't cleanly separate into altune's `track`/`artist`/`album` `ResultKind`s at multi-result volume, so — like Discogs (rate-limited) and TheAudioDB (1-result cap) — it never joins the `SearchProvider` fan-out. Its only wired role is as one link in the artwork-resolution chain (`buildArtworkChain`, `internal/app/search_wiring.go` — see [artwork-chain](artwork-chain.md)), positioned after the identity-based/MBID sources and before the always-on name-search sources. The architecture doc also flags a "✅ song credits 📋" cell for Genius — a mapped-but-unbuilt capability (Genius's song-credits data), separate from lyrics and not present in the code today. No `docs/providers/genius.md` exists yet.

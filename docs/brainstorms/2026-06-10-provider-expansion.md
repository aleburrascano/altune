---
date: 2026-06-10
status: active
graduates-to: docs/specs/provider-expansion-v1/spec.md
related:
  - docs/brainstorms/2026-06-10-provider-specialized-discovery.md
  - docs/brainstorms/2026-06-10-discovery-polish-v1.md
  - docs/adr/0007-unified-music-search.md
---

# Brainstorm — Provider expansion (5 new providers)

## Problem

The current 6-provider stack has three gaps that hurt the user experience:

1. **Artist images**: TheAudioDB searches by name, returning wrong photos
   for common names ("Che" returns a rock band instead of the Atlanta rapper).
   Need MBID-based image lookup.
2. **Cross-provider identity**: MB URL lookup is the only bridge between
   provider IDs. Sparse coverage, 1 req/s rate limit. Need a broader hub.
3. **Catalog completeness**: MusicBrainz is accurate but incomplete for
   smaller/newer artists. Deezer has more but conflates same-name artists.
   Need a third catalog source.

## Decision

Add 5 new providers in 4 phases:

### Phase 1 — Artist images (Fanart.tv)

- **Auth**: Free API key (self-register at fanart.tv)
- **Rate limit**: Generous (personal key avoids project throttling)
- **What it gives us**: Artist thumbnails, backgrounds, logos, banners —
  ALL by MBID. No name-based lookup. "Che" + correct MBID = correct photo.
- **Role**: Primary artist image source, replaces TheAudioDB for artists.
  TheAudioDB stays as fallback for album/track artwork.
- **Covers Fanart.tv gap**: Mainstream-focused. Underground artists may not
  have coverage — fall through to Genius (phase 4) then TheAudioDB.

### Phase 2 — Identity bridge (Wikidata)

- **Auth**: None
- **Rate limit**: ~5 req/min sustained for SPARQL queries
- **What it gives us**: Cross-provider ID translation in one query.
  `Deezer ID → MBID → Discogs ID → Wikimedia Commons image URL`.
  Properties: P434 (MBID), P2722 (Deezer), P3040 (SoundCloud), P1953 (Discogs).
- **Role**: Replaces MB URL lookup as the identity bridge. Broader coverage,
  richer data (also returns Wikimedia Commons artist images as fallback).
- **Caching**: Aggressive — ID mappings are stable. Cache Wikidata results
  for 30+ days.

### Phase 3 — Catalog depth (Discogs)

- **Auth**: App-level API key in .env (60 req/min); OAuth for images
- **Rate limit**: 60 req/min authenticated
- **What it gives us**: 16M releases. Exceptional for credits, liner notes,
  physical releases. Fills gaps where MB and Deezer are incomplete.
  Genre/style tags (more granular than MB's).
- **Role**: Third catalog source. Union with MB + Deezer for discography.
  Primary source for credits/personnel data (future feature).
- **Image note**: Artist images require authenticated requests even after
  getting the URL. Alternative: use Discogs artist ID → Wikidata → Wikimedia
  image (avoids the auth-for-images constraint).

### Phase 4 — Enrichment (Genius + AcoustID + ListenBrainz)

**Genius**:
- Auth: Bearer token (free dev account)
- What: Artist images (strong for hip-hop), page-view popularity, song
  metadata, featured artists
- Role: Supplementary artist images (fills Fanart.tv gaps for underground
  hip-hop), additional popularity signal

**AcoustID**:
- Auth: Free API key
- Rate limit: 3 req/sec
- What: Audio fingerprint → MBID. After yt-dlp downloads a track,
  fingerprint it to confirm correct identification.
- Role: Post-acquisition verification (not discovery — different feature surface)

**ListenBrainz**:
- Auth: None for reads
- What: Listen counts, similar-artist data, genre tags
- Role: Additional popularity signal alongside Last.fm. Similar-artist
  discovery for future "related artists" feature.

## Provider role map (current → after)

| Role | Current | After |
|------|---------|-------|
| Search/streaming | Deezer, iTunes, SoundCloud | Same |
| Identity | MusicBrainz + MB URL lookup | MusicBrainz + **Wikidata** |
| Discography | MB + Deezer union | MB + Deezer + **Discogs** |
| Artist images | TheAudioDB (name-based, broken) | **Fanart.tv** (MBID) + **Genius** + TheAudioDB |
| Popularity | Last.fm | Last.fm + **Genius** + **ListenBrainz** |
| Credits | MusicBrainz | MusicBrainz + **Discogs** |
| Audio verification | None | **AcoustID** |

## Out of scope

- Paid APIs (Spotify, Musixmatch full lyrics)
- Bandcamp as a search provider (fragile scraping; works for acquisition)
- AcousticBrainz (deprecated 2022)
- Lyrics display (future feature, would use Genius page links)
- User-facing provider attribution ("powered by Discogs" badges)

## Risks

- **Fanart.tv coverage for underground artists** — may return nothing for
  artists like "Che." Mitigation: fall through to Genius → TheAudioDB.
- **Wikidata SPARQL rate limits** — 5 req/min sustained. Mitigation:
  aggressive caching (ID mappings rarely change), batch queries.
- **Discogs OAuth complexity** — more setup than API-key auth.
  Mitigation: standard OAuth1.0a, well-documented, one-time setup.
- **TheAudioDB free tier ToS** — prohibits app store deployment.
  Mitigation: Fanart.tv + Genius + Wikimedia images reduce TheAudioDB
  dependence. Evaluate paid tier ($8/mo) if needed for production.

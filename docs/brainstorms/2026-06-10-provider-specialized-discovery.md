---
date: 2026-06-10
status: active
graduates-to: implementation (focused, no spec needed)
related:
  - docs/brainstorms/2026-06-10-identifier-only-merge.md
  - docs/brainstorms/2026-06-09-discovery-foundation-rework.md
  - docs/specs/discovery-identity-v1/spec.md
---

# Brainstorm — Provider-specialized discovery

## Problem

Deezer conflates multiple real-world artists with the same name under a
single artist ID. Searching "Che" returns Deezer artist 234701081, whose
`/artist/{id}/albums` endpoint returns 80 items including releases from
1960s-era artists unrelated to the modern Atlanta rapper. Deezer also
returns an empty-hash placeholder image (`d41d8cd98f00b204e9800998ecf8427e`)
for this artist — no profile picture.

MusicBrainz has the correct data: MBID `0a68f3b5-79c2-4f81-a7bc-ebc977602e86`
is human-curated as "Che, Atlanta rapper" and its release-groups contain only
that artist's actual discography.

The root cause: treating all providers equally for all tasks. Each provider
excels at different things.

## Decision

Use each provider for its strength:

| Role | Provider | Why |
|------|----------|-----|
| Identity (who is this artist?) | MusicBrainz | Human-curated MBIDs, separate entries per real-world artist |
| Discography (what did they release?) | MusicBrainz (primary), Deezer (fallback) | MB's release-groups are per-MBID, not conflated |
| Streaming links / audio | Deezer, SoundCloud | Actual playable content |
| Artwork | TheAudioDB (primary), Deezer (fallback) | TheAudioDB has curated artist images; Deezer often empty |
| Popularity signal | Last.fm | Uniform play counts across providers |

## Changes

### 1. Artist discography: MB-primary when MBID available

**Current:** `useArtistContent` fans out to all sources on the tapped card.
If the card has a Deezer source, it queries Deezer's `/artist/{id}/albums`
which returns polluted data.

**New:** When the tapped artist card has an MB source (or an MBID resolved
via URL lookup), use MB's release-groups as the authoritative album list.
Fall back to Deezer only when no MB source is available.

This is a frontend change in `apps/mobile/src/features/detail/hooks/useArtistContent.ts`:
prefer the MB source for albums, Deezer source for top tracks (MB has no
popularity-ranked top tracks).

### 2. Artist image enrichment

**Current:** `ChainedArtworkResolver` runs on tracks/albums during
`SearchMusic._enrich` but NOT on artist results.

**New:** During enrichment, also resolve artwork for artist results that
have no image (or have Deezer's empty placeholder). TheAudioDB's
`search.php?s=<artist_name>` returns artist photos when available.

This is a backend change in `services/api/src/altune/application/discovery/search_music.py`:
extend `_enrich` to cover artist `image_url` backfill.

### 3. No changes needed for

- **Search merge logic** — the hybrid merge (identifier-only for artists,
  JW for tracks/albums) already works correctly.
- **Album track listings** — `useAlbumTracks` already fetches from the
  specific provider source on the card.
- **MBID URL lookup** — already runs in the enrichment phase.

## Out of scope

- New providers (Discogs, Spotify)
- UI disambiguation cards ("Other artists named X")
- Merge pipeline rewrites — the current hybrid merge is correct
- Rewriting the quality scorer or removing infrastructure constants

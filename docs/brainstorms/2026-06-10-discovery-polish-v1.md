---
date: 2026-06-10
status: active
graduates-to: implementation (targeted fixes, no spec needed)
related:
  - docs/brainstorms/2026-06-10-provider-specialized-discovery.md
  - docs/brainstorms/2026-06-10-identifier-only-merge.md
  - docs/brainstorms/2026-06-09-discovery-foundation-rework.md
---

# Brainstorm — Discovery polish v1

## Problem

After two rework iterations (foundation-v1 + identity-v1), search ranking
regressed and the discography is still partially incomplete. Specific issues:

1. **Ranking regression**: "Super Shy" lo-fi version ranks above NewJeans
   genuine. Caused by removing keyword demotion + diluting multi-source
   agreement into a composite score. The old sort key had multi_source as
   a direct binary tiebreaker; the new one buries it in a 4-signal average.

2. **Incomplete discography**: MB-primary is clean but incomplete for
   smaller artists (e.g., "The Final Agenda" by Che, 2022, missing from
   MB but available on Deezer/Spotify).

3. **Wrong artist image**: TheAudioDB searches by artist name, returns
   wrong "Che" band photo for the Atlanta rapper.

## Bar

Between Spotify-parity and "80% right, no embarrassments." Top result
usually correct, discography complete and clean, no wrong-artist content.
This is shipping to friends and family.

## Decisions

### D1 — Restore multi-source + popularity as direct sort key tiebreakers

The quality score composite dilutes the most important signals. Restore
the pre-foundation sort key positions for multi-source (binary: >1 provider)
and popularity (0-1 float) as direct tiebreakers ABOVE the quality composite.

New sort key: relevance-band → record-type demotion → multi-source →
popularity → quality score → RRF → alphabetical.

This fixes the Super Shy regression without keywords: the genuine track
(multi-source, high popularity) naturally ranks above the lo-fi version
(single-source, low popularity).

### D2 — Union discography: MB primary + Deezer supplement

When the artist card has both MB and Deezer sources:
1. Fetch albums from MB (clean, MBID-scoped)
2. Fetch albums from Deezer
3. Title-match dedup: any Deezer album NOT in the MB list gets appended
4. This gives MB's accuracy + Deezer's completeness

When only one source exists, use that source alone.

### D3 — Fix artist image: skip TheAudioDB for artist-by-name

TheAudioDB's `search.php?s=<name>` is unreliable for common names. For
the artist profile picture, prefer the image from Deezer's search result
(which comes from the correctly identified artist page). If Deezer has
the empty-hash placeholder, try TheAudioDB — but only when the artist
name is sufficiently unique (>= 2 words, or has a disambiguation signal).
For single-word common names like "Che", accept no image over wrong image.

### D4 — Restore content-token relevance scoring

The `_content_tokens` path in `_relevance_score` was removed during the
identity-v1 rework. It handled article mismatches ("The Weeknd" vs
"Weeknd") by scoring on content tokens (stopwords dropped). This is a
structural pattern, not a keyword list. Restoring it fixes relevance
band accuracy for queries containing articles/common words.

## Changes

1. **dedup.py sort key**: restore multi_source + popularity positions
2. **useArtistContent.ts**: union fetch (MB + Deezer dedup)
3. **search_music.py enrichment**: skip TheAudioDB for single-word artists
4. **dedup.py _relevance_score**: restore content-token scoring path

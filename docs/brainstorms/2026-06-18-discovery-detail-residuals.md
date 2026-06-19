---
date: 2026-06-18
topic: discovery-detail-residuals
parent: artist-detail-quality-requirements.md
---

# Discovery Detail Residuals — P1-P4

## Summary

Four issues surfaced after the artist-detail-quality implementation session. P1 and P2 are identity-verification gaps in the enrichment and disambiguation pipelines. P3 is a frontend cache/wiring issue. P4 is a UX gap when multiple artists share the same name.

---

## Problem Frame

The artist-detail-quality session (R1-R11) implemented MusicBrainz cross-referencing, artist disambiguation, and artwork fallbacks. All 498 tests pass. But testing with the real app on "Che" revealed four residual issues: wrong artwork from a same-name artist, unverified disambiguation text, stale frontend state, and a wall of identical artist results in search.

---

## Requirements

### P1: Artwork identity — reorder disambiguation before enrichment

**Root cause:** `applyArtistDisambiguation` sets the MBID on artist results (line 496 of `search_music.go`), but `enrich` reads the MBID for Fanart.tv artwork resolution (line 569). Current order: `enrich()` → `Rerank()` → `applyArtistDisambiguation()`. Fanart.tv never fires because MBID isn't set yet. TheAudioDB/Deezer artwork resolvers search by name, returning images for the wrong same-name artist.

- **R1.** Reorder the Execute pipeline to: `applyArtistDisambiguation()` → `enrich()` → `Rerank()`. This sets MBID before enrichment, allowing Fanart.tv (MBID-based) to fire first.
- **R2.** After reorder, the artwork cache key includes MBID — results with different MBIDs get different cached artwork. Verify cache key includes MBID (it does: `artworkCache.Get` already accepts `mbid` param).
- **R3.** When Fanart.tv returns artwork for an MBID, skip TheAudioDB/Deezer name-based resolvers entirely (already the behavior via early return in `resolveArtwork`).

### P2: Disambiguation accuracy — validate MB match against Deezer data

**Root cause:** `resolveArtistMBID` picks the first exact name match from MusicBrainz's artist search. For "Che", MB returns multiple matches. The first one may be the wrong artist, and its disambiguation ("Atlanta rapper") may be incorrect for the Deezer artist being viewed.

- **R4.** When `ResolveArtistIdentity` gets multiple exact name matches from MB, score each candidate by comparing its release-groups against the Deezer artist's known albums. Pick the candidate with the highest album overlap count.
- **R5.** If no candidate has any album overlap (all are 0), fall back to the current behavior (first match) but mark the disambiguation as `low_confidence` in extras so the frontend can decide whether to display it.
- **R6.** Cache the resolution result per (normalized artist name) with a 30-day TTL (existing MBID cache behavior).

### P3: Frontend discography — debug and fix stale state

**Root cause:** Backend correctly separates confirmed vs unconfirmed albums (verified via API: `&name=Che` returns 8 confirmed first, 17 unconfirmed last). Frontend may not be picking up the changes due to: (a) Expo dev server not hot-reloading the changed files, (b) React Query cache (30min staleTime), or (c) frontend's own filtering logic conflicting.

- **R7.** Verify the mobile app sends `&name=<artistName>` in the album fetch request. Check `useArtistContent.ts` → `getArtistAlbums()` → network request. If missing, the backend falls back to Deezer-only order.
- **R8.** If the request is correct, clear React Query cache for artist-albums queries and verify the response matches the direct API call.
- **R9.** Remove any frontend-side MB-authoritative filtering that may conflict with the backend's validation (the backend already handles confirmed/unconfirmed ordering).

### P4: Artist spam — disambiguation grouping

**Root cause:** Searching "Che" returns 7 separate artist results with the same name. R1 from the hardening session correctly prevents merging (artists only merge on MBID). CollapseVersions correctly skips artists. But the UX shows a wall of identical entries.

- **R10.** After diversity enforcement, group artist results that share the same normalized name. Keep the highest-popularity artist as the primary result. Collapse the remaining same-name artists into a single "N other artists named X" placeholder.
- **R11.** The placeholder carries metadata: count of collapsed artists, and the collapsed results themselves (so the frontend can expand them). Wire format: an extra field `collapsed_artists: [{title, subtitle, sources, ...}]` on the primary result.
- **R12.** The primary artist result's subtitle must show disambiguation text (from R5/R6 or P2). If no disambiguation is available, use the artist's top track title as a differentiator.
- **R13.** Frontend: render the "N other artists" row as a tappable expansion. When tapped, show the collapsed artists inline with their own disambiguation subtitles.

---

## Acceptance Examples

- **AE1 (P1).** Search "Che". The Atlanta rapper's result shows Fanart.tv or correct Deezer artwork, not TheAudioDB's rock band image. Verify by checking `enrich.artwork` debug log shows `source: fanart` for the artist with MBID.
- **AE2 (P2).** Search "Che". The primary artist result shows "Atlanta rapper" as subtitle — verified correct because the MB candidate's release-groups overlap with Deezer albums (REST IN BASS, Sayso Says).
- **AE3 (P3).** On the phone, tap the "Che" artist result. Discography shows REST IN BASS, Sayso Says, closed captions first. Samsonite, Gallos Ciegos are at the bottom (unconfirmed section).
- **AE4 (P4).** Search "Che". Shows 1 "Che — Atlanta rapper" artist result + "6 other artists named Che" row. Tapping expands to show the other artists with their own subtitles.
- **AE5 (regression).** Search "Drake" — single artist result, no grouping (only one Drake). Search "Aurora" — may show grouping if multiple Aurora artists exist.

---

## Success Criteria

- "Che" search shows correct artwork for the Atlanta rapper (not a rock band).
- Disambiguation text is verified against album overlap, not blindly taken from first MB match.
- Phone displays confirmed albums first, unconfirmed last on the artist detail screen.
- Same-name artists are grouped with expansion, not shown as a wall of duplicates.
- All existing tests (498) continue to pass.
- No regression on "Drake", "Kendrick Lamar", "Bad Bunny" positioning tests.

---

## Scope Boundaries

**In scope:** P1-P4 fixes only.

**Out of scope:**
- New providers (Discogs, AllMusic, Fanart.tv for non-artist artwork)
- Ranking algorithm changes (already hardened)
- User-facing "report wrong artist" functionality
- Search result grouping beyond same-name artists (e.g., grouping albums by artist)

---

## Key Decisions

- **Reorder over redesign for P1** — swapping two lines (`applyArtistDisambiguation` before `enrich`) fixes the artwork issue without rearchitecting the enrichment chain. The MBID-based Fanart.tv resolver already exists and already short-circuits name-based resolvers.
- **Album overlap scoring for P2** — comparing MB candidates' release-groups against Deezer albums is the most reliable cross-reference available. It reuses the existing `ValidateArtistAlbums` infrastructure.
- **Grouping over limiting for P4** — limiting to top-2 risks hiding the user's target artist. Grouping preserves all results while cleaning up the UX. The frontend work is bounded (one expandable row component).
- **Backend-driven grouping for P4** — the backend has popularity data and disambiguation text. Grouping in the backend keeps the frontend simple (render what it gets) and ensures consistency.

---

## Implementation Priority

1. **P3 first** — may be a zero-code fix (Expo restart + cache clear). Verify before writing code.
2. **P1 second** — one-line reorder in `Execute()`. Highest impact per effort.
3. **P2 third** — extends existing `ResolveArtistIdentity` with candidate scoring. Medium effort.
4. **P4 last** — new pipeline stage + frontend component. Largest effort but self-contained.

---

## Dependencies / Assumptions

- Fanart.tv API returns correct artwork for a given MBID (verified: Fanart.tv indexes by MBID, which is authoritative).
- MusicBrainz returns release-groups for artist MBIDs (verified: existing `fetchReleaseGroups` in `musicbrainz.go` already does this).
- React Query cache invalidation works as expected with `queryClient.invalidateQueries`.
- The "Che" artist's MBID (`0a68f3b5-79c2-4f81-a7bc-ebc977602e86`) is correct and resolves to the Atlanta rapper in MB.

---

## Outstanding Questions

### Deferred to Planning

- [Affects R10][Technical] Where in the pipeline does artist grouping happen — after `EnforceDiversity` or as part of it? Grouping needs access to final popularity scores.
- [Affects R11][Technical] Wire format for collapsed artists — array in extras vs separate field on SearchResultDTO.
- [Affects R13][Frontend] Expand/collapse animation and state management for the "other artists" row.

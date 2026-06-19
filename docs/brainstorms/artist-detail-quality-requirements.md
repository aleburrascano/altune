---
date: 2026-06-18
topic: artist-detail-quality
---

# Artist Detail Quality — Multi-Provider Validation

## Summary

Use MusicBrainz as an identity anchor for artist detail screens. Cross-reference Deezer's album data against MB release-groups to filter misattributed content, surface disambiguation text in search results, and fall back to album artwork when artists lack profile images.

---

## Problem Frame

The detail screen currently queries Deezer only for an artist's albums and top tracks. Deezer's data for niche artists is frequently contaminated — multiple real-world artists with the same name get their releases dumped under a single Deezer artist ID. Searching "Che" and tapping the correct result (Deezer ID 234701081, Atlanta rapper, 8829 fans) shows "Samsonite", "Kiss Me in the Sky", "Gallos Ciegos", and "Tšernobõl" mixed in with the real discography — releases that belong to different artists named "Che" in different countries and genres.

MusicBrainz has this artist cataloged with a unique MBID (`0a68f3b5-79c2-4f81-a7bc-ebc977602e86`) and a disambiguation field ("Atlanta rapper"). MB's community-curated release data is authoritative — if MB says a release belongs to this MBID, it does. This data is sitting unused.

Meanwhile, search results show 7 entries all named "Che" with no way to tell them apart — no disambiguation text, no genre, no top track preview. And niche artists often have no profile image from any provider.

---

## Requirements

**MBID resolution on detail screen**
- R1. When the detail screen loads for an artist, resolve the artist's MBID via MusicBrainz artist search (by name). Cache the result.
- R2. If an MBID is resolved, query MusicBrainz for the artist's release-groups (albums/EPs/singles) in parallel with the existing Deezer query.
- R3. Cross-reference Deezer albums against MB release-groups by normalized title. Albums confirmed by both providers sort first. Deezer-only albums sort last.
- R4. When no MBID is resolved (artist too niche for MB), fall back to Deezer-only behavior unchanged.

**Disambiguation in search results**
- R5. If a search result carries a MusicBrainz `disambiguation` extra field, surface it as subtitle text for artist results (e.g., "Che — Atlanta rapper").
- R6. If no disambiguation is available but the artist has a top track, use that as disambiguation (e.g., "Che — BA$$, REST IN BASS").

**Artwork fallback**
- R7. When an artist has no profile image after enrichment, use the cover art of their highest-popularity album as a fallback.

**Testing**
- R8. Manual test with "Che" — discography should show REST IN BASS, Sayso Says, closed captions at the top; Samsonite, Gallos Ciegos, Tšernobõl deprioritized or absent.
- R9. Manual test with "Drake" — no regression, discography should be unchanged.
- R10. Regression test: artist detail with MBID returns MB-confirmed albums before Deezer-only albums.
- R11. Regression test: artist detail without MBID falls back to Deezer-only.

---

## Acceptance Examples

- AE1. **Covers R1, R2, R3.** Given artist "Che" (Deezer 234701081), when the detail screen loads, the system resolves MBID `0a68f3b5...`, queries MB for release-groups, and cross-references. "REST IN BASS" appears in both → top of list. "Gallos Ciegos" is Deezer-only → bottom of list or filtered.
- AE2. **Covers R4.** Given an artist with no MB match, the detail screen shows Deezer albums in their default order — identical to current behavior.
- AE3. **Covers R5.** Given search results for "Che", the MB-sourced artist shows "Atlanta rapper" as subtitle. Other Che entries show their own disambiguation or nothing.
- AE4. **Covers R7.** Given artist "Che" with a Deezer placeholder image, the detail screen shows the cover of "REST IN BASS" as the artist image.

---

## Success Criteria

- "Che" detail screen shows correct discography (REST IN BASS, Sayso Says, etc.) without Samsonite/Gallos Ciegos mixed in.
- "Drake" detail screen is unchanged (regression check).
- Artist search results for "Che" show disambiguation text distinguishing the Atlanta rapper from the Korean singer-songwriter.
- Niche artists without profile images show album art instead of a blank placeholder.
- All existing discovery tests continue to pass.

---

## Scope Boundaries

- Not adding new providers (Discogs, AllMusic, etc.) — future work. See: https://en.wikipedia.org/wiki/List_of_online_music_databases
- Not changing the search ranking algorithm (that was done in the hardening session)
- Not implementing user-facing "report wrong album" functionality
- Not caching MB release data long-term (start with per-request, optimize later)

---

## Key Decisions

- **MusicBrainz as identity anchor** rather than multi-provider consensus: MB is the only provider with authoritative artist identity + disambiguation. TheAudioDB didn't have "Che" at all. Consensus across 3 weak sources is worse than 1 authoritative source.
- **Deprioritize rather than hide** Deezer-only albums: some may be real releases MB hasn't cataloged yet. Hiding risks false negatives.
- **Album artwork as profile fallback** rather than placeholder: the artist's music is right there — using its cover is better than a blank image.

---

## Dependencies / Assumptions

- MusicBrainz API rate limit: 1 req/sec with user-agent identification. The detail screen fires once on tap, not on every search — this is well within limits.
- MB artist search may return multiple matches for common names. Picking the right one requires matching against the existing Deezer data (fan count, genre, top tracks).
- MBID resolution should be cached (Redis, 30-day TTL) to avoid repeated MB lookups for the same artist.

---

## Outstanding Questions

### Deferred to Planning

- [Affects R1][Technical] How to disambiguate between multiple MB matches for the same name (e.g., 5 "Che" entries in MB) — match by release overlap? by fan count proximity?
- [Affects R3][Technical] What normalized title similarity threshold should be used for cross-referencing Deezer albums against MB release-groups?
- [Affects R7][Technical] Which album's cover art to use — highest popularity, most recent, or first album?

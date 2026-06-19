# Handoff: Discovery Pipeline Hardening — Session Results

**Date:** 2026-06-18
**From:** Audit + implementation session
**To:** Next session — deeper brainstorm on artist identity and data quality

## What was done

### Audit (completed)
Full read of all 74 Go files in `services/go-api/internal/discovery/`. Produced 17 findings across 4 severity levels. See the full audit report in conversation context.

### Implementation (completed, all tests pass — 498/498)

**Critical fixes:**
- **Removed artist name-only merge** (dedup.go) — artists now only merge on MBID. Same-name artists stay as separate results. This was the root cause of wrong detail screen content.
- **Added vocabulary ingestion quality gate** (search_music.go) — results with popularity < 30 are no longer ingested. Errors from `vocabStore.Add` are now logged instead of discarded.
- **CollapseVersions now skips artists** (dedup.go) — prevents same-name artist results from being collapsed into one.

**Other fixes (all 20 requirements from the brainstorm doc):**
- MusicBrainz disambiguation field extracted
- 3 dead functions deleted (ApplyIntentBoost, dedupRelatedGroups, preQueryCorrection)
- Enum zero-value sentinels added (ResultKindUnknown, ProviderUnknown at iota 0)
- PopularityDominance condition rewritten with named booleans
- Provider Search() methods now log per-kind errors
- levenshteinDistance and maxLevenshtein fixed to operate on runes
- context.Background() removed from vocabulary_store.go
- SetNoisePatterns global mutable state eliminated
- boostIfRecent float64 precision preserved
- EnforceDiversity off-by-one fixed
- VocabularyEntry.Kind typed as VocabularyKind

### Requirements doc
`docs/brainstorms/discovery-pipeline-hardening-requirements.md`

## What was verified

Search "Che" now returns 7 separate artist results, each with their own Deezer ID, ranked by popularity. `variant_count` is 1 for all (no false collapsing). The correct "Che" (Deezer 234701081, 8829 fans) ranks #1.

Canonical test suite: 498 tests pass, 0 failures. `go vet` clean.

## What is NOT solved — the real remaining problems

The fixes above solved the *pipeline* bugs (wrong merge, vocabulary pollution, dead code). But the user tested and found three **data quality** problems that the pipeline cannot fix by itself:

### 1. Split artist identity across provider entries

The same real artist "Che" exists as multiple Deezer entries:
- ID 234701081 (8829 fans) — has "REST IN BASS", "BA$$", "MANNEQUIN"
- ID 234701531 (52 fans) — has "#RESIDE", "The Final Agenda"

Both are the same person. The user confirmed "#RESIDE" and "The Final Agenda" are real releases by their "Che". But because Deezer has them under different artist IDs, they show as separate results with incomplete discographies.

**This is not unique to "Che"** — any niche artist with fragmented provider data will have this problem. The pipeline currently has no mechanism to detect that two different provider entries refer to the same real artist when they have different external IDs and no shared MBID.

### 2. Contaminated discography from Deezer

Even the "correct" Deezer entry (234701081) has albums that don't belong to this artist:
- "Samsonite" (2020) — not by the user's "Che"
- "Kiss Me in the Sky" (2020) — not by the user's "Che"

This is Deezer's own data quality issue — they've attributed other artists' albums to this artist ID. The pipeline faithfully passes through what Deezer returns from `/artist/{id}/albums`.

**Possible approaches (for brainstorm):**
- Cross-reference album artist names against the searched artist name
- Use MusicBrainz release data to validate album attribution
- Show a confidence indicator on each album in the discography

### 3. No artwork for niche artists

Deezer returns a placeholder image (empty MD5 hash `d41d8cd98f00b204e9800998ecf8427e`) for artist 234701081. The enrichment chain (Fanart.tv → Genius → Deezer → iTunes) can't find artwork either — the artist is too niche for any provider to have a photo.

MusicBrainz returned 0 results for "Che", so there's no MBID to look up Fanart.tv with.

**Possible approaches (for brainstorm):**
- Use the artist's album artwork as a fallback profile image
- Extract artwork from Last.fm (which has track images for this artist)
- Accept placeholder for artists below a fan threshold

### 4. Result spam for ambiguous queries

Searching "Che" returns 7 artist results all named "Che" — correct behavior (they're separate artists) but bad UX. The user sees a wall of identical-looking entries with no way to tell which is theirs.

**Possible approaches (for brainstorm):**
- Surface the MusicBrainz disambiguation field as a subtitle on artist results
- Show fan count or genre as disambiguation text
- Collapse same-name artists into a disambiguation UI ("7 artists named Che — which one?")
- Limit same-name artists to top 2-3 in blended view

## Key files for next session

Same as previous handoff, plus:
- `docs/brainstorms/discovery-pipeline-hardening-requirements.md` — requirements for the fixes done today
- `services/go-api/internal/discovery/service/dedup.go` — now without name-only merge or artist collapsing
- `services/go-api/internal/discovery/service/search_music.go` — now with vocab quality gate

## Brainstorm completed (same session)

The brainstorm happened in the same session. Key discovery: **MusicBrainz has the user's "Che" cataloged** as MBID `0a68f3b5-79c2-4f81-a7bc-ebc977602e86` with disambiguation "Atlanta rapper". This is the identity anchor.

Deezer's album list for ID 234701081 is heavily contaminated — releases in Spanish ("Gallos Ciegos"), Estonian ("Tšernobõl"), and other languages mixed in with the real discography. These belong to different real-world artists all mapped to the same Deezer ID.

### Agreed approach: MusicBrainz as identity anchor

See `docs/brainstorms/artist-detail-quality-requirements.md` for the full spec. Summary:
1. On artist detail screen, resolve MBID via MusicBrainz
2. Cross-reference Deezer albums against MB release-groups
3. MB-confirmed albums sort first; Deezer-only albums deprioritized
4. Surface MB disambiguation as subtitle in search results
5. Use album artwork as profile image fallback
6. Fall back to Deezer-only when MB doesn't have the artist

### Resource for future provider expansion
Wikipedia list of online music databases: https://en.wikipedia.org/wiki/List_of_online_music_databases
Candidates for future integration: Discogs, AllMusic, Rate Your Music — all have structured artist/release data.

## Recommended next session approach

1. **Start from the requirements doc** — `docs/brainstorms/artist-detail-quality-requirements.md`
2. **Implement MBID resolution + album cross-reference** on the detail screen first (R1-R4)
3. **Test manually** with "Che" and "Drake" before implementing disambiguation and artwork fallback
4. **Resolve the outstanding questions** during planning: how to pick the right MB match among multiple same-name artists, what title similarity threshold for cross-reference

## How to run

```bash
cd services/go-api

# Build
go build -o ./tmp/api.exe ./cmd/api

# Tests
go test ./internal/discovery/... -count=1 -timeout=120s

# Run with debug logging
LOG_LEVEL=debug ./tmp/api.exe
```

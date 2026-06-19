# Handoff: Discovery Pipeline Audit & Enhancement

**Date:** 2026-06-18
**From:** Backend audit session (full codebase read, 906 tests passing)
**To:** Next session — focused exclusively on the discovery pipeline

## Start here

Invoke these skills in order:
1. `/audit-codebase backend` — focused on `services/go-api/internal/discovery/` only
2. `/ce-brainstorm` — with the findings, discuss whether to simplify, add ML, or restructure

## The problem

The discovery pipeline has been built incrementally over many sessions. Each session added layers — intent detection, correction, vocabulary store, popularity dominance, diversity enforcement, recency boost, version collapse, quality scoring. The result is a 12-stage pipeline (see `services/go-api/internal/discovery/ARCHITECTURE.md` mermaid diagram) that works well for mainstream queries but breaks unpredictably for:

1. **Short/ambiguous names** — "Che" (underground hip-hop artist) doesn't show up, or shows the wrong artist's profile picture, or pollutes discography with other artists also named "Che"
2. **Artist identity confusion** — when a user taps an artist result, the detail screen (album list, top tracks) can show content from a *different* artist with the same name. The Deezer external ID attached to the merged result may belong to the wrong "Che"
3. **Regression instability** — results that worked previously stop working with no code change. Likely caused by vocabulary store pollution, cache state, or provider response variation

The user's core frustration: **"this should just work for any artist without me having to be the developer fixing it."** A normal user who searches "Che" and gets wrong results would just leave the app. The pipeline needs to be robust enough that it handles edge cases algorithmically, not through manual per-artist fixes.

## What was done this session

### Fixes applied (already committed)
- Error logging added to 13 silent 500 handler paths
- errors.Is comparisons fixed (6 locations)
- Acquisition matching: title-only penalty (0.6x), artist-channel preference
- Artwork: iTunes added to enrichment chain, limit raised 25→50
- Entity-relationship search enrichment (FindRelatedService)
- Track status polling endpoint
- Per-stage pipeline tests + canonical ranking regression suite
- Dynamic DB-driven integration tests

### Key files (read these first)
- `services/go-api/internal/discovery/ARCHITECTURE.md` — mermaid diagram of all 12 stages
- `services/go-api/internal/discovery/service/search_music.go` — main orchestrator (~310 lines)
- `services/go-api/internal/discovery/service/dedup.go` — FuseAndRank, ranking key, merge logic (~890 lines)
- `services/go-api/internal/discovery/service/normalize.go` — 8-step text normalization
- `services/go-api/internal/discovery/service/intent.go` — vocabulary-based artist+track split
- `services/go-api/internal/discovery/service/correction.go` — trigram + phonetic correction
- `services/go-api/internal/discovery/service/popularity.go` — log-scale popularity normalization
- `services/go-api/internal/discovery/service/quality_scorer.go` — completeness + IsDemoted
- `services/go-api/internal/discovery/service/query_clean.go` — YouTube noise stripping
- `services/go-api/internal/discovery/service/find_related.go` — entity relationship enrichment
- `services/go-api/internal/discovery/adapters/providers/deezer.go` — primary provider (search + content + artwork)
- `services/go-api/CLAUDE.md` — pipeline testing discipline, canonical query list, design decisions

### Existing regression test files
- `services/go-api/internal/discovery/service/ranking_regression_test.go` — 11 regression tests
- `services/go-api/internal/discovery/service/ranking_canonical_test.go` — 9 canonical query tests
- `services/go-api/internal/discovery/service/pipeline_stages_test.go` — per-stage unit tests
- `services/go-api/internal/discovery/service/pipeline_integration_test.go` — dynamic DB-driven tests
- `services/go-api/internal/discovery/service/dedup_test.go` — merge/dedup unit tests
- `services/go-api/internal/discovery/service/search_music_test.go` — orchestrator unit tests

## Suspected root causes for the "Che" problem

### 1. Artist merge is too aggressive (dedup.go:196-208)
Artists without MBID are merged by normalized name alone:
```go
if a.Kind == domain.ResultKindArtist && mbidA == "" && mbidB == "" {
    normA := NormalizeForMatch(a.Title)
    normB := NormalizeForMatch(b.Title)
    if normA != "" && normA == normB {
        return mergeResults(a, b, domain.ConfidenceMedium, domain.EntityResolutionNone), true
    }
}
```
This means "Che" the hip-hop artist and "Che" the rock band get merged into ONE result. The merged result takes the Deezer external ID of whichever had more complete metadata — which may be the wrong "Che". When the user taps into the detail screen, the detail endpoint fetches content using that wrong external ID.

### 2. No artist disambiguation metadata
Providers return limited metadata for artists: name, fan count, maybe genre. There's no "genre" or "disambiguation" field being used to tell apart two artists with the same name. MusicBrainz has disambiguation fields ("Che (Italian hip hop artist)" vs "Che (rock band)") but they're not being extracted.

### 3. Short query problems compound
"Che" is 3 characters. The `contentWords` function (dedup.go:114) requires tokens of length >= 2, so "che" passes. But the sharesWord gate does exact word matching, which means results need "che" as a standalone word in their title or subtitle. Some providers may not return useful results for such short queries, and the ones that do may be the wrong "Che."

### 4. Vocabulary pollution may cause regression instability
The vocabulary store ingests the top 5 search results after every search (search_music.go:287). If a search for "Che" returns the wrong artist, that wrong artist gets ingested into the vocabulary. Future corrections and intent detection may then reinforce the wrong result. This could explain why "it worked, then broke" — a bad search polluted the vocabulary.

## Questions the next session should answer

1. **Is the pipeline over-engineered?** 12 stages is a lot. Which stages actually earn their keep? Can any be removed without regression? (Run the canonical test suite after removing each stage individually.)

2. **Should artist merge be more conservative?** The name-only merge without MBID is the likely cause of the "Che" discography pollution. Options: require genre match, require fan-count similarity, or just don't merge artists without identifier overlap.

3. **Should we add MusicBrainz disambiguation to artist results?** MB returns a `disambiguation` field that distinguishes "Che (rapper)" from "Che (rock band)." This could prevent wrong merges and improve the detail screen.

4. **Is vocabulary ingestion poisoning results?** The fire-and-forget vocabulary ingest (search_music.go:287) has no quality gate. If wrong results get ingested, they reinforce themselves.

5. **Would ML help here?** Potential uses: learned ranking instead of hand-tuned multi-criteria sort, embedding-based artist disambiguation, query understanding. But also: the current issues may be solvable with simpler fixes (conservative merge, better metadata).

6. **Are there hardcoded values that should be configurable?** Constants scattered across the pipeline: `identityMin=60`, `enrichLimit=50`, `diversityWindow=10`, `maxPerArtistInTop=3`, `popularityDominanceGapAbs=20`, `rrfK=60`, `versionSimilarityThreshold=85`, `recencyWindowDays=30`, `relatedTopN=5`. Should these be in config?

## Pipeline stage inventory (for the audit)

| # | Stage | File | Purpose | Risk |
|---|-------|------|---------|------|
| 1 | NormalizeForMatch | normalize.go | 8-step text canonicalization | Low — well-tested |
| 2 | CleanQuery | query_clean.go | Strip YouTube noise | Low — hardcoded patterns |
| 3 | DetectIntent | intent.go | Vocabulary-based artist+track split | Medium — depends on vocab quality |
| 4 | Provider scatter | search_music.go | Parallel 6-provider search (1.5s timeout) | Low |
| 5 | FuseAndRank | dedup.go | Identifier merge + multi-criteria sort | **High** — artist merge too loose |
| 6 | NormalizePopularity | popularity.go | Log-scale 0-100 normalization | Low |
| 7 | Recency boost | dedup.go | 1.1x for releases < 30 days | Low |
| 8 | CollapseVersions | dedup.go | Group remix/live variants | Medium — similarity threshold |
| 9 | PopularityDominance | dedup.go | Cross-kind top-5 promotion | Medium — can flip wrong result to #1 |
| 10 | EnforceDiversity | dedup.go | Max 3 per artist in top 10 | Low |
| 11 | Enrich | search_music.go | Artwork + popularity for top 50 | Low |
| 12 | Rerank | dedup.go | Re-sort after enrichment | Low |
| 13 | FindRelated | find_related.go | Entity relationship enrichment | Low — new, separate section |
| 14 | Correction | search_music.go | Zero-results-only query correction | Medium — depends on vocab quality |

## How to run the existing tests

```bash
cd services/go-api

# All discovery tests
go test ./internal/discovery/... -count=1 -v

# Canonical ranking regression suite
go test ./internal/discovery/service/... -run TestCanonicalRanking -v -count=1

# Pipeline stage tests
go test ./internal/discovery/service/... -run TestStage_ -v -count=1

# Dynamic DB-driven integration tests (needs DATABASE_URL)
DATABASE_URL=... go test ./internal/discovery/service/... -run TestPipelineIntegration -v -timeout=120s

# Full suite
go test ./internal/... -count=1 -timeout=120s
```

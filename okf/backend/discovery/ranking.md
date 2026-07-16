---
type: Subsystem
title: Discovery ranking
description: Layer 3's parameter-free relevance scoring (rank.go, rank_relevance.go) plus the experimental tail-demotion and cross-kind-prominence tiebreaks, and post-rank diversity/collapse reshaping (diversity.go).
resource: services/go-api/internal/discovery/service/rank.go, services/go-api/internal/discovery/service/rank_relevance.go, services/go-api/internal/discovery/service/diversity.go
tags: [discovery, ranking, relevance, diversity, eval-gated]
verified_commit: 6a047a008fb23b38e719d9a9a3e9b539ab349d4d
---

`Rank`/`rankWithProminence` (rank.go) orders merged `Entity` values (see [merge-dedup](merge-dedup.md)) by continuous, parameter-free relevance — no relevance bands, popularity-dominance window, kind tiers, or intent contract. Pass 1 applies eligibility gates: `sharesQueryWord` (drops results sharing no content token with the query) and `hasBrowseableSource` (artist/album need a Deezer source; tracks always pass). Pass 2 scores each eligible entity via `scored{relevance, behavioral, prominence, pop, rrf, multi, demoted}` and `rankLess` sorts by: demotion flag → relevance → cross-kind prominence (kind-difference gated) → behavioral score → popularity → multi-source → RRF (`rrfK=60`) → stable subtitle/title tiebreak.

Relevance itself (`rank_relevance.go`, `idfWeightedCoverage`) is `Σ_qtoken IDF(token)·bestFuzzyMatch(token, title+subtitle) / Σ_qtoken IDF(token)`: IDF weight (`queryTokenRarity` in rank.go, `rarity = 1 - documentFrequency/N` over the eligible candidate set) comes from the data, not a tuned constant, so a repeated "artist" token in an "artist title" query weighs ~0 while the rare "song" token weighs ~1; `bestTokenSimilarity` is a normalized Levenshtein ratio, continuous with no cutoff. When every query token is ubiquitous (IDF can't distinguish), it falls back to `symmetricSimilarity` — the published token-sort ratio — so an exact title still beats a superset title. This replaced the pre-rebuild `distinguishingBoostWeight = 0.35` tuned constant.

Two eval-gated experimental rungs, both default-off and inert unless their composition-root env flag is set: `isLowConfidenceTail` (tail-noise demotion — flags single-source SoundCloud/Last.fm results with no ISRC/MBID/album as low-confidence tail, sorting below every corroborated result; `TailNoiseInTopK` is the tracked quality signal) and `prominenceOf` (cross-kind prominence — log-compressed Deezer `nb_fan`/`rank`, breaking relevance ties only *between different kinds* so a famous bare-name artist rises above a same-name track without touching track-vs-track order).

`diversity.go`'s `EnforceDiversity` (cap `maxPerArtistInTop=3` within `diversityWindow=10`) and `CollapseArtistDuplicates` (fold same-name artist duplicates, keying by MBID when the name is ambiguous per `ambiguousArtistNames`) are explicitly **product policy**, not the query-fit constants the rebuild purged — they're exempt from the zero-arbitrary-constants doctrine and validated via the `cmd/discoveryeval -mode diversity` harness (which uses `rankPipelineNoReshape` as its baseline — see [eval-harness](eval-harness.md)).

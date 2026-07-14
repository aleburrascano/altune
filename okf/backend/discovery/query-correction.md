---
type: Subsystem
title: Discovery query correction & suggest
description: Post-zero-result "did you mean" correction (correction.go, search_correction.go), query cleanup (query_clean.go), phonetic matching (metaphone.go), and autocomplete (suggest.go), all backed by the learned vocabulary store.
resource: services/go-api/internal/discovery/service/correction.go, services/go-api/internal/discovery/service/query_clean.go, services/go-api/internal/discovery/service/metaphone.go, services/go-api/internal/discovery/service/suggest.go
tags: [discovery, correction, autocomplete, phonetics, query-normalization]
verified_commit: 6a047a008fb23b38e719d9a9a3e9b539ab349d4d
---

`CleanQuery` (query_clean.go) strips noise phrases users paste from video titles ("official music video", "lyrics", "hq"/"4k"/"1080p", etc. — `noisePatterns`) and a *trailing* dangling "feat"/"ft"/"featuring" (`trailingFeatRe` — mid-typing residue with no featured artist after it, which otherwise makes providers expand into phantom composite-artist rows). A mid-query "feat" is left intact since it precedes a real featured artist. This runs first in `Service.Execute` (see [[scatter-gather]]), before normalization and fan-out.

`CorrectionService` (correction.go) is the "did you mean" engine, entirely vocabulary-driven (`ports.VocabularyStore`, see [[vocabulary]], no hardcoded word banks — "no hardcoded workarounds" per the discovery `CLAUDE.md` testing discipline). `Correct` (whole-query only, used in a disabled pre-correction phase) and `CorrectAggressive` (whole-query then token-level fallback, used post-zero-result) both call `vocab.FindClosest` for trigram/phonetic candidates and `pickBestCorrection`, which picks the candidate with the smallest Levenshtein distance under `maxCorrectionDist` (1 edit for ≤4 runes, 2 for ≤8, 3 beyond — length-scaled, not a single tuned cutoff). `search_correction.go`'s `Service.tryCorrection` is the integration point: it fires only when the first search returns zero results (a principled trigger, not a tuned relevance threshold — the weaker "did you mean" for non-empty-but-weak results was removed as query-fit), re-runs `CorrectAggressive`, and re-executes the full fan-out/merge/rank/enrich pipeline on the corrected query. Per discovery `CLAUDE.md`, pre-correction is disabled (it rewrote valid queries from vocabulary pollution); post-correction alone measures 93% recall / 100% precision.

`DoubleMetaphone`/`MetaphoneKey` (metaphone.go) is a simplified English-pronunciation phonetic coder (silent-letter handling, digraphs like "PH"→F, "TH"→0, "SH"/"CH"→X) that lets the vocabulary store's fuzzy search catch spelling variants that share a sound but not a spelling ("Kanye"/"Kanye West" phonetic drift); it's consumed as a `MetaphoneFunc` injected into `adapters/cache/vocabulary_store.go`'s Redis store, not called directly from `correction.go`.

`SuggestService` (suggest.go) powers autocomplete: `SuggestByPrefix` first (fast prefix match), and if under `limit`, `supplementWithFuzzy` tops up with `FindClosest` fuzzy candidates, deduplicated by `TermNorm` (`deduplicateSuggestions`).

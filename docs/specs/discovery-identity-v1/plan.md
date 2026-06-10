# discovery-identity-v1 — implementation plan

Spec: docs/specs/discovery-identity-v1/spec.md

## Slices

### Slice 1: Strip _try_merge to identifier-only paths
- Acceptance criterion: AC#1, AC#2, AC#3
- Files:
  - `services/api/src/altune/application/discovery/dedup.py` (edit — remove JW fallthrough from _try_merge; keep only ISRC and MBID paths; MBID merges unconditionally)
  - `services/api/tests/unit/altune/application/discovery/test_dedup.py` (edit — add test: same name, no shared ID → separate)
- Failing test first: `test_same_name_no_shared_identifier_stays_separate`
- Verify: `pytest tests/unit/altune/application/discovery/test_dedup.py -v`

### Slice 2: Remove all JW/duration/prior constants from dedup.py
- Acceptance criterion: AC#4 (partial)
- Files:
  - `services/api/src/altune/application/discovery/dedup.py` (edit — remove _JW_HIGH, _JW_MEDIUM, _JW_ARTIST, _DURATION_TOLERANCE_S, _duration_of, _fallback dict in _winning_prior)
  - `services/api/src/altune/domain/discovery/entity_resolution_tier.py` (edit — remove FUZZY and DURATION_CONFIRMED members)
- Failing test first: `test_entity_resolution_tier_has_three_members` (MBID, ISRC, NONE)
- Verify: `pytest tests/unit/altune/domain/discovery/ tests/unit/altune/application/discovery/test_dedup.py -v`

### Slice 3: Replace keyword demotion with record_type canonical set
- Acceptance criterion: AC#8, AC#9
- Files:
  - `services/api/src/altune/application/discovery/quality_scorer.py` (edit — replace is_demoted: remove _COVER_PHRASE_RE, _COVER_WORD_RE, _DEMOTED_TYPES; check record_type against canonical set {"album","single","ep"})
  - `services/api/tests/unit/altune/application/discovery/test_fuse_and_rank.py` (edit)
- Failing test first: `test_demotion_by_record_type_not_keywords`
- Verify: `pytest tests/unit/altune/application/discovery/test_fuse_and_rank.py -v`

### Slice 4: Replace match gate with relevance > 0
- Acceptance criterion: AC#10, AC#11
- Files:
  - `services/api/src/altune/application/discovery/dedup.py` (edit — replace _passes_gate with relevance_score > 0.0 check; remove _content_tokens, _genuine_split_exists, _is_bootleg)
  - `services/api/src/altune/application/discovery/quality_scorer.py` (edit — remove significant_tokens, _FUNCTION_WORDS)
- Failing test first: `test_zero_relevance_filtered_nonzero_passes`
- Verify: `pytest tests/unit/altune/application/discovery/ tests/eval/ -v`

### Slice 5: Replace _winning_prior with quality score
- Acceptance criterion: AC#4 (complete — canonical selection + sort key)
- Files:
  - `services/api/src/altune/application/discovery/dedup.py` (edit — _merge uses quality_score for canonical selection instead of _winning_prior; remove _winning_prior entirely)
- Failing test first: `test_no_winning_prior_in_dedup_module`
- Verify: `pytest tests/unit/altune/application/discovery/ tests/eval/ -v`

### Slice 6: Add MbidResolver port + MB URL lookup enrichment
- Acceptance criterion: AC#5, AC#6, AC#7
- Files:
  - `services/api/src/altune/application/discovery/ports.py` (edit — add MbidResolver protocol)
  - `services/api/src/altune/application/discovery/search_music.py` (edit — call MbidResolver in enrichment phase for results without MBID)
  - `services/api/src/altune/adapters/outbound/discovery/musicbrainz/adapter.py` (edit — add resolve_mbid_from_url method)
  - `services/api/tests/unit/altune/application/discovery/test_search_music.py` (edit)
- Failing test first: `test_mbid_resolver_stores_resolved_mbid_in_extras`
- Verify: `pytest tests/unit/altune/application/discovery/test_search_music.py -v`

### Slice 7: Constrain useArtistContent to card sources only
- Acceptance criterion: AC#12, AC#13, AC#14
- Files:
  - `apps/mobile/src/features/detail/hooks/useArtistContent.ts` (edit — remove CONTENT_PROVIDERS, remove SC name fan-out; use only sources from the result card)
- Failing test first: N/A (no existing useArtistContent tests; verify by running the app)
- Verify: `cd apps/mobile && npx jest --verbose`

### Slice 8: Remove remaining adapter constants + update eval
- Acceptance criterion: AC#4 (adapter cleanup), AC#15
- Files:
  - `services/api/src/altune/adapters/outbound/discovery/soundcloud/adapter.py` (edit — remove _SET_NOISE_RE)
  - `services/api/src/altune/adapters/outbound/discovery/lastfm/adapter.py` (edit — replace _PLAYCOUNT_MAX_LOG10 with dynamic max)
  - `services/api/tests/eval/golden_cases.py` (edit — adjust cases for identifier-only merge)
  - `services/api/tests/eval/test_ranking_quality.py` (edit — lower baseline to 0.85)
  - `docs/ubiquitous-language.md` (edit — update EntityResolutionTier to 3 members)
- Verify: `pytest tests/eval/ -v`

## Risks
- Slice 4 (removing bootleg detection + match gate) is highest risk for eval regression
- Slice 5 (removing _winning_prior) changes canonical selection for ALL merges
- Run eval harness after every slice

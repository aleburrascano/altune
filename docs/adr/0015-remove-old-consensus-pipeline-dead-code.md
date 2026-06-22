# ADR-0015: Remove dead code from the retired consensus discovery pipeline

- **Status:** Accepted
- **Date:** 2026-06-22
- **Deciders:** solo + Claude
- **Context tags:** [layer | cleanup]

## Context

A `/tighten-backend` review found a cluster of discovery code that the pre-2026-06-20 pipeline redesign (identity-resolution → consensus, then the ADR-0007 strangler collapse) left unwired. `deadcode ./...` reported it unreachable from `main`; cross-package reference search confirmed the only remaining callers were the modules' own tests:

- **`adapters/cache/` subtree** — `query_cache.go`, `mbid_cache.go`, `popularity_cache.go`, `fetch_success.go`, `discogs_cache.go` (≈329 LOC). Constructed only by `redis_integration_test.go`; never wired in `internal/app/` or `cmd/`. These were the caches of the retired consensus/popularity pipeline. The live caches (`artwork_cache`, `enrichment_cache`, `vocabulary_store`) are untouched and their integration tests retained.
- **`adapters/providers/wikidata.go`** (≈116 LOC) — a Wikidata SPARQL Deezer→MBID resolver, superseded by the MusicBrainz url-relation bridge ([ADR-0011]). Constructed only by its own test.
- **`service/url_router.go` `DetectProvider`** — zero references anywhere (URL-based provider detection replaced by explicit per-result provider routing).
- **`service/consensus.go` `WithCacheTTL`** — an unused functional option.
- **`adapters/cache/vocabulary_store.go` `AllKeys` + three `*ForTest` exports** — `AllKeys` was a test-cleanup helper (moved into the test file); the three `ForTest` key exposers had zero references.
- **`service/mock_ports_test.go`** — mocks for retired ports (`SearchProvider`, `PopularityResolver`, `ArtworkResolver`, `ArtworkCache`).

The brake here is "test-only ≠ delete": correct, high-quality code that a future spec might revive. The counter-weight: this code has sat unwired across the *entire* redesign, the redesign is documented as the current architecture (`CLAUDE.md` discovery section, [ADR-0007] collapse addendum), and carrying it imposes a standing comprehension + compile cost with no consumer. Per the deletion test, removing it makes no production complexity reappear.

## Decision

Delete the listed dead code. Record the removal here so a future `/tighten-backend` run does not re-surface it as "missing caching" and so the abandoned Wikidata-bridge approach is remembered rather than silently lost.

If query/consensus caching or a Wikidata identity fallback is wanted later, re-introduce it deliberately against the current identity-resolution pipeline — the retired implementations were keyed to the old fan-out shape and would need re-keying anyway.

## Alternatives considered

| Alternative | Why not |
|---|---|
| Keep the cache subtree for an eventual re-wire | Speculative; unwired across the whole redesign. YAGNI — re-add against the real pipeline when a measured need appears. |
| Keep `wikidata.go` as a fallback resolver | No call path; the MB url-relation bridge ([ADR-0011]) is the chosen mechanism. Documented here as an abandoned approach instead of carried as dead weight. |
| Keep the test-only domain funcs too (`ParseAlbumVerdict`, `ExtractISRCRegistrant`, `ContentValidationStatus.String`) | **Kept** — these encode ubiquitous-language domain vocabulary and the project enforces glossary↔code parity; deleting domain terms to satisfy a coverage tool is the wrong trade. Out of scope of this removal. |

## Consequences

### What becomes easier
- `adapters/cache/` now contains only wired caches; the discovery surface a reader must understand shrinks by ≈450 LOC + dead tests.

### What becomes harder
- A future query/popularity cache starts from scratch rather than reviving these files. Accepted — the old shape no longer fits.

### What we're committing to (and the cost to reverse)
- Reversal is `git revert` of the three cleanup commits; low cost, but a deliberate re-wire is the expected path, not revert.

## Vault references

- [vault: wiki/concepts/YAGNI Principle.md]
- [vault: wiki/concepts/Coupling and Cohesion.md]

## Related

- Supersedes the dead remnants of [ADR-0007] (unified music search) and its 2026-06-21 strangler-collapse addendum
- Identity bridge of record: [ADR-0011] (identity-based merge)
- Sibling layout decision: [ADR-0014] (keep discovery service/ layout; note: `url_router.go`, referenced there as a util, is deleted here as dead)
- Surfaced by: `/tighten-backend` review, 2026-06-22

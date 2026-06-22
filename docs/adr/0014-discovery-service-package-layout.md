# ADR-0014: Keep the discovery ranking/util code in service/; defer a domain/ranking module

- **Status:** Accepted
- **Date:** 2026-06-22
- **Deciders:** solo + Claude
- **Context tags:** [layer | pattern]

## Context

A `/tighten-backend` review flagged the discovery `service/` package (34 files) as having logical rather than functional cohesion: it mixes port-driven use cases with pure functions that import no ports — the ranking decisions (`merge.go`, `rank.go`, `diversity.go`, collapse) and string/url utilities (`metaphone.go`, `query_clean.go`, `url_router.go`). Two moves were proposed: (S1) relocate the pure utilities to `domain/`, and (G2) gather the ranking decisions into a deep `domain/ranking` module behind a small `Rank(...)` interface, leaving `service/` only the port-touching orchestration.

Two forces push back. First, `domain-layer.md` explicitly bans "Manager/Helper types in domain/" — `metaphone`/`query_clean`/`url_router` are infrastructure-flavoured helpers, not domain model, so moving them into `domain/` would trade one purity complaint for a worse one. Second, the ranking pipeline is the product's core IP and is under active redesign (identity-resolution → consensus); `CLAUDE.md`'s "Key files" index documents these files at their current `service/` paths. Reshaping module boundaries mid-redesign churns against work in motion and staleness the documented map.

## Decision

Keep the discovery ranking decisions and pure utilities in `service/` for now. Do not move helpers into `domain/` (S1 rejected — it would violate the domain-layer no-helpers rule). Defer the `domain/ranking` extraction (G2) until the consensus-ranking redesign settles; revisit it then as a deliberate, tested reshape rather than a layout tidy.

## Alternatives considered

| Alternative | Why not |
|---|---|
| Move `metaphone`/`query_clean`/`url_router` to `domain/` | They are helpers, not domain model; `domain-layer.md` bans helper types in the domain layer. |
| Move them to a new `discovery/service/<util>` sub-package | Premature: single-context helpers with no second consumer; a new package for them is structure for its own sake (YAGNI). |
| Extract `domain/ranking` now | Touches the hottest path (every search) and its largest test surface while that exact logic is being redesigned. High churn risk against in-flight work for an organizational gain. |

## Consequences

### What becomes easier
- The documented `CLAUDE.md` key-files map stays accurate; the ranking redesign proceeds without a concurrent boundary move.

### What becomes harder
- `service/` stays large; finding a specific use case among 34 files still leans on naming. Accepted as a navigation cost, not a correctness one.

### What we're committing to (and the cost to reverse)
- Revisit `domain/ranking` once consensus-ranking is stable. The extraction is additive (move pure functions + their tests, add a `Rank` facade); reversal cost is low because no data or API contract changes.

## Vault references

- [vault: wiki/concepts/Coupling and Cohesion.md]
- [vault: wiki/concepts/Modularity.md]
- [vault: wiki/concepts/YAGNI Principle.md]

## Related

- Predecessor: ADR-0007 (unified music search), and its 2026-06-21 strangler-collapse addendum
- Surfaced by: `/tighten-backend` review, 2026-06-22 (findings S1, G2)

> **Note (2026-06-22, ADR-0015):** `url_router.go`, cited above as a `service/` util to keep, was subsequently deleted as dead code — its `DetectProvider` had zero references. The "keep helpers in `service/`" decision stands for `metaphone.go` and `query_clean.go`.

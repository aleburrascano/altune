# ADR-0011: Cross-provider identity-based merge

- **Status:** Accepted (eval 2026-06-22: top-3 **99.4%** (1782/1792) ≥ baseline 1773 — no regression)
- **Date:** 2026-06-21 (accepted 2026-06-22)
- **Deciders:** solo + Claude
- **Context tags:** [pattern | policy]

## Context

Discovery `Merge` (Layer 2) decides "same entity?" by shared identifier (ISRC, then
MBID — and only when *both* results already carry one) and otherwise by exact canonical
title+artist. Non-MB providers (Deezer, iTunes, SoundCloud) never carry an MBID, so an MB
result and a Deezer result for the same entity can merge **only by name** today. MusicBrainz
asserts each entity's Deezer/Spotify/Discogs id via `url-relations`; the
`musicbrainz-enrichment` feature already extracts those (`MBEnrichment.ExternalIDs`) and
caches them per `(kind, mbid)`. That id graph is a stronger same-entity signal than name
similarity, but it was extracted and returned, never fed into merge (`musicbrainz.md` §4 cap 4,
§8 #1). This decision wires it in.

## Decision

Add an `IdentityBridge` port — `ExternalIDs(ctx, kind, mbid) → map[provider]id` — backed by
the **existing** enrichment cache (the `RedisEnrichmentCache` gains the method; no new MB
call, no new store). Before `Merge`, the orchestrator stamps each MB-sourced result's bridged
ids into `extras["xref"]`. `sameEntity` gains one **additive** branch: two results merge when
one *claims* a `(provider, id)` the other carries natively, recorded as a new
`EntityResolutionBridge` tier (identity-grade → high confidence). The branch only ever merges;
it never blocks a name match, so the change is purely additive. Bridge coverage is whatever
detail-opens have warmed into the cache; a cache miss (or no Redis) degrades silently to the
prior name-only merge.

## Alternatives considered

| Alternative | Why not |
|---|---|
| Inline `url-rels` lookup per MB result during search | Pays MusicBrainz's 1 req/sec wall on the hot search path — the exact cost the detail-open enrichment design avoided. |
| Background job pre-warming the full bridge | Real coverage win, but a new worker + warm strategy. YAGNI until the cache-only version's eval shows coverage is too thin. Noted as the graduation path. |
| Identity as an *override* (un-merge same-name different-entity) | Riskier — could split results the eval expects merged. The distinct-MBID short-circuit already keeps MB-vs-MB entities apart; the cross-provider keep-apart is a separate, later increment. |

## Consequences

### What becomes easier
- An MB result and a Deezer/iTunes result for the same entity merge by stated identity even when titles differ (transliterations, alternate names) — cleaner, deduped cards.
- The cross-provider id graph becomes a live merge signal, not just display data; coverage compounds as entities are opened.

### What becomes harder
- Merge behavior now depends on cache warmth — bridge merges appear only for enriched entities, so the same query can merge differently before vs after an entity is opened. The `merge.identity_bridge_stamped` debug log makes this observable.

### What we're committing to (and the cost to reverse)
- A ranking-affecting change, so it is gated by `discoveryeval -mode eval -top-k 3`: it ships only if the top-3 library eval shows no regression. Reverting is a one-line removal of `WithIdentityBridge` in `search_wiring.go` (the bridge becomes nil → name-only merge); the port/tier/method stay harmless.

## Implementation notes

- Built behind the gate: domain `EntityResolutionBridge`, `ports.IdentityBridge`,
  `RedisEnrichmentCache.ExternalIDs`, the `sameEntity` bridge branch, the `stampIdentities`
  pass, and `WithIdentityBridge` wiring. Unit tests green.
- **Gate result (2026-06-22):** `go run ./cmd/discoveryeval -mode eval -top-k 3 -concurrency 1`
  (MB-healthy run) → top-3 **1782/1792 (99.4%)**, the highest of any recorded run (baselines
  1773–1775), with only 10 entities outside top-3 (baseline 19). No regression → Accepted. Result
  held despite worse-than-baseline provider conditions that day (YouTube Music slow, MB timeouts).

## Vault references

- [vault: wiki/concepts/Domain-Driven Design.md] — entity identity / resolution.
- [vault: wiki/concepts/Hexagonal Architecture.md] — `IdentityBridge` as a consumer-defined port.

## Related

- Provider audit: `docs/providers/musicbrainz.md` §4 (cap 4), §8 (#1 — the next-step this implements).
- Predecessor spec: `docs/specs/musicbrainz-enrichment/spec.md` (extracted `external_ids`; deferred merge-use to here).
- Ubiquitous language: `EntityResolutionTier` (adds `bridge` member).

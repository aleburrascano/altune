---
title: "refactor: Discovery strangler rebuild — categorical layers, zero arbitrary constants, top-K gate"
type: refactor
status: active
date: 2026-06-20
revised: 2026-06-20
origin: docs/brainstorms/2026-06-20-discovery-rebuild-architecture.md
depends_on: docs/plans/2026-06-20-002-feat-discovery-telemetry-eval-step-zero-plan.md
---

# refactor: Discovery strangler rebuild — categorical layers, zero arbitrary constants, top-K gate

## Summary

Rebuild the discovery pipeline as a clean five-layer architecture in a **new package**, layer
by layer behind the existing handler, **gated at every step by the top-K eval from Step Zero**,
and **never deleting the old package** (kept as reference + instant rollback). The distinctive
goal beyond a tidier shape: **replace tuned, query-fit constants with categorical, structural
decisions.** Reuse only the clean parts (value objects, provider adapters); redesign the
decision logic (merge, rank) from the ground up. No ML, no acquisition pipeline — separate
threads with clean seams.

---

## The design doctrine (why this rebuild, in one rule)

**Zero arbitrary, query-fit constants.** Not zero numbers — zero numbers that were fit to a
handful of queries and that nobody can re-derive.

A constant appears whenever a *continuous* or *multi-signal* judgment is forced into a decision
("is this similar enough to be the same song?" → a threshold; "when does popularity beat
relevance?" → an exchange rate). Those judgments are unavoidable. There are exactly three ways
to make one:

1. **Hand-tuned constant** — e.g. `TokenSortRatio ≥ 85`. Untraceable, rots, fit to a few
   queries. **This is what we are removing.**
2. **Learned weight** — the number becomes a model parameter fit to data. Needs labels we don't
   have yet, and *hides* the number in a model. **Deferred** (the telemetry from plan 002 is the
   groundwork; the Layer-3 seam is where it lands later).
3. **Categorical / structural decision** — restructure so the judgment becomes a category and
   the number disappears or shrinks to a documented last resort. **This is the strategy now.**

Each surviving number must be justified as one of: **principled** (a published convention or an
SLA choice — e.g. provider timeouts, RRF's `k=60`), **learned-later** (parked at the ML seam),
or **last-resort** (a single fuzzy fallback the eval proves generalizes). Anything else is a
band-aid and is removed.

A clarifying corollary from the product owner (2026-06-20): **the correct answer does not have
to be #1 — it must be visible in the top results.** The eval therefore measures **top-K**, not
only top-1. A categorical tier model satisfies this *structurally*: a same-named album sits in
the tier immediately below the exact track, so it lands right under the right answer without any
tuning.

---

## Baseline evidence (captured this session, the gate we must hold)

Measured by the library-derived eval (plan 002 U4) on the full production catalog (1,792 distinct
`(title, artist)` entities, cloned prod → dev 2026-06-20):

- **Top-1: 97.2%** (1,742 / 1,792). **Top-3: 98.9%** (1,773) — the **gate metric** (product bar:
  the right answer must be *visible*, not strictly #1). ≈**99.4%** true at top-3 after excluding
  the ~9 `¥$` eval-matcher artifacts (symbol-only artist normalizes to empty; pipeline is correct).
  (Numbers from the top-K run 2026-06-20; small run-to-run variance from live providers.)
- The **31 entities that pass at top-3 but not top-1 are exactly Pattern A** (album at #1, the
  correct track at #2) — i.e. Pattern A is *already acceptable under the top-3 bar*; the tier model
  promotes the track to #1 as polish. Only **19 entities miss top-3 entirely**, of which ~9 are
  `¥$` matcher artifacts, 2 are Pattern B sequels, and ~8 are genuinely hard (obscure/cross-artist).
- The **46 failures are not random** — they are three nameable patterns at three different
  layers, each with an identified mechanism in today's code:

| Pattern | Count | Mechanism today | Layer |
|---|---|---|---|
| **A — same-named album outranks the track** | 17 | relevance rounds into the same `0.05` band (`roundBand`), so popularity breaks the "tie"; `ApplyPopularityDominance` can also promote the album on a `gap ≥ 20 / ≥ 3×` | **3 (rank)** |
| **B — numbered sequel collapses into the original** | 8 | `CollapseVersions` merges titles with `TokenSortRatio ≥ 85` and keeps the more-popular one, so "Shotta Flow 2" is deleted as a "version" of "Shotta Flow" | **2 (dedup)** |
| **C — obscure track replaced by the artist's hit** | ~7 | the exact track is likely absent from the candidate set (a coverage hole, almost certainly the YouTube Music 0-results bug) | **1 (coverage)** |

This is the proof that the failures are *structural*, not a long tail of special cases — and that
each is fixed by a categorical decision, not a new constant.

---

## Problem Frame

The current pipeline works (~98%) but has accumulated ~13 sequential transforms and a scatter of
tuned constants across many sessions, with no way to tell which generalize. (Verified: the
band-aids are tuned constants + stage sprawl, **not** hardcoded artist hacks — origin §1, §13.)
The fix is a strangler-fig rebuild whose decisions are **categorical** and whose every step is
**gated by the top-K eval**. This is step 1+ of the strangler; step 0 (the eval + telemetry
instrument) is plan 002 and is complete.

---

## Prerequisite (hard dependency)

**Plan 002 (Step Zero) is complete.** Its outputs gate this plan's cutover:
- The **top-K eval** (002 U4, extended with top-K — see 002) produces the current-pipeline
  baseline the new pipeline must match or beat before any cutover.
- **Coverage signals A/B** (002 U5/U6) baseline coverage (signal A stays blind until client
  telemetry accrues; signal B is live).
- The **telemetry store** (002 U1) exists; the new services re-emit into it (U7).

Building the new pipeline in isolation (U1–U6) can proceed now; **no traffic flips** until a
recorded baseline delta shows new ≥ old on the chosen K.

---

## Near-term parallel tasks (turn the telemetry faucet on during the rebuild)

These run *alongside* the rebuild so the testing phase banks a real, clean dataset for ML (plan 004),
instead of wasting it. They are not gated on the rebuild.

- **[DONE 2026-06-20] Apply migration 004 to Supabase** — `discovery_events` now exists in prod;
  search telemetry persists (it was silently dropping before). `uuid-ossp` was already present.
- **[DONE 2026-06-20] Stamp `pipeline_version` on search telemetry** — every search event now carries
  `pipeline_version` (`v1` today; the rebuilt pipeline emits `v2`). Makes transition-phase telemetry
  attributable so ML trains per-pipeline and labels aren't mixed across the cutover.
- **[TODO — separate mobile slice] Client telemetry emission.** The dense behavioral signals
  (play / skip / completion / library-add / wrong-album) are fired by the mobile client → the existing
  ingest endpoint (`POST /v1/discovery/events`, plan 002 U3). Nothing calls it yet. **This is the
  gating data-collection task for ML (plan 004 Stage 1).** Until it lands, signal-a is blind and
  ranking ML has no training data. Do it early/in parallel — it does not depend on the rebuild.
- **When the rebuilt pipeline emits telemetry (U7),** stamp it `pipeline_version = "v2"`.

---

## Requirements

- R1. New pipeline in a **new package**, clean Layers 0–4, not modifying the old package. *(origin §4, §13)*
- R2. **Strangler cutover**: the handler routes each surface (search, artist albums, album
  tracks, top tracks) old-or-new via a per-surface flag, default old. *(origin §13)*
- R3. **The old package is never deleted** during the rebuild — reference + rollback. *(origin §13, D7)*
- R4. Every cutover is **gated green on the top-K eval** and shows no coverage regression. *(origin §12, §13)*
- R5. **Decisions are categorical.** Merge and rank use identifier/structure/tier decisions; the
  only surviving thresholds are principled, learned-later, or a single documented last-resort —
  each justified in a constants ledger. *(doctrine; origin §1, D10)*
- R6. Rebuild Layers 1 (fan-out), 2 (merge/entity-resolution), 3 (rank), and Stage-3 consensus
  (with a per-artist cache). Reuse provider adapters and domain value objects **verbatim**. *(origin §4, §9)*
- R7. Close the **three coverage gaps** (YT Music 0-results, long-tail track fallback,
  underground top-track fallback). *(origin §5)*
- R8. Re-add **telemetry emission** in the new services. *(origin §8)*
- R9. Deterministic only — **no model code**; Layer 4 acquisition is a **handoff seam**. *(origin §6, §4 L4)*

**Origin:** `docs/brainstorms/2026-06-20-discovery-rebuild-architecture.md` — §4 (layers), §5 (coverage), §12 (eval gate), §13 (strangler), §9 (cache).

---

## Scope Boundaries

- No ML / model code (the Layer-3 scorer is a deterministic function — the future ML seam).
- No acquisition pipeline (yt-dlp→OCI) — Layer 4 is an interface/handoff only.
- No new providers — reuse the adapters in `internal/discovery/adapters/providers/`.
- No mobile/client changes.

### Deferred to Follow-Up Work

- **Removal of the old package** — only after the new pipeline runs in production on all surfaces
  for a sustained period, at the user's explicit decision. This plan *retains* old code (R3).
- **Final package rename** (provisional `discovery2` → `discovery`) — bundled with old-package removal.
- **ML scorers** at the Layer-3 seam, **provider-selection / contamination ML** — future (origin §6).
- **Client-side telemetry emission** (mobile) — separate slice; until it lands, coverage signal A
  has no demand-side data.

---

## The categorical layer design (the heart)

Each layer replaces a continuous/tuned decision with a structural one. The current magic numbers
and their disposition are in the constants ledger below.

### Layer 0 — Query understanding (make intent *authoritative*)
Normalize, then parse an explicit structured intent `{artist?, title?, kind?}`. Today
`DetectIntent` exists but feeds a small additive `intentBoost` that gets rounded away. **Change:**
intent becomes a *contract* downstream trusts — it selects the relevance tier in Layer 3, it is
not a score nudge. No threshold.

### Layer 1 — Coverage fan-out (complete the candidate set)
All providers in parallel, each with its own timeout (**principled** SLA number) + circuit
breaker. Reuse adapters verbatim. **Fix the three coverage gaps (R7)** — most importantly the
YouTube Music 0-results bug, the likely cause of Pattern C. No ranking constants live here.

### Layer 2 — Merge + entity resolution (a categorical cascade, not one fuzzy threshold)
Decide "same entity?" by a cascade that consults the cheap/exact signals first:
1. **Identifier match** — same MBID or same ISRC → same entity. *Exact; no threshold.*
2. **Version-marker categories** — parse a title into `(core, version-tag)` where version-tags are
   categorical: sequel number (`2`, `3`, `Pt. 2`), `(Remix)`, `(feat. X)`, `(Live)`, `(Deluxe)`.
   A different core **or** a different distinguishing tag → **different entity**. This generalizes
   the existing collab guard (which already refuses to merge "Song" with "Song (feat. X)") to all
   version markers — and **dissolves Pattern B** without a similarity number.
3. **Fuzzy title+artist** — the **only** surviving threshold, used as a *last resort* for the
   typo residual after 1–2 decide nothing. Low-stakes; documented; eval-gated.

### Layer 3 — Disambiguate + rank (lexicographic tiers, not banded scores)
Order by **categorical relevance tiers**, popularity only *within* a tier:
- **T1 — exact intent match**: artist ✓ + full title ✓ + kind matches the Layer-0 intent.
- **T2 — exact title, different kind**: the same-named album/single — sits *immediately below* T1.
- **T3 — partial match.**
- **T4 — weak / none.**
- Within a tier: **popularity, then multi-source (RRF)** — preserving the genuine "popularity >
  multi-source" decision, but it can never lift a lower tier above a higher one.

This **dissolves Pattern A** (the exact track is T1; the same-named album is T2, right below it —
exactly the product bar) and removes the `0.05` band, the dominance `gap/factor`, and the additive
`intentBoost` — three tuned constants gone, replaced by tier categories.

### Stage-3 consensus (detail screen, already mostly categorical)
2+ providers → confirmed; single → unconfirmed; MB authority filter rejects contamination. Carry
forward the audited engine (bounded timeout, deterministic merge) and add the per-artist cache.

### The gate
The **top-K eval** runs after each layer. Cutover requires **new ≥ old at the chosen K** (default
**top-3**, the product bar) with **top-1 tracked alongside**, and no coverage regression.

---

## Constants ledger (every magic number, with its fate)

| Constant (today) | Where | Disposition |
|---|---|---|
| `versionSimilarityThreshold = 85` | `CollapseVersions` | **Replace (categorical):** identifier-first + version-marker categories; fuzzy only last-resort. |
| `roundBand` 0.05 relevance band | `rankingKeyLess`/`Rerank` | **Remove:** lexicographic tiers, no band. |
| `popularityDominanceWindow=5, GapAbs=20, GapFactor=3.0` | `ApplyPopularityDominance` | **Remove:** cross-kind order is structural (T1 vs T2). |
| `intentBoost` | `FuseAndRank` | **Replace:** intent selects the tier, not a score nudge. |
| `consensusTitleMatchMinTSR = 85` | `consensus` | **Replace (categorical):** same as version cascade. |
| `rrfK = 60` | RRF | **Keep (principled):** published convention; role shrinks to within-tier tiebreak. |
| provider timeouts (1.5s), `consensusTimeout=10s` | fan-out / consensus | **Keep (principled):** SLA choices. |
| `clickBoostAmount = 0.03` | `applyClickBoost` | **Learn-later:** behavioral signal → ML seam; **drop for v1**. |
| `artistSourceBonus = 5` | `effectivePop` | **Reconsider:** make categorical (multi-source artist tier) or drop. |
| `positionalPopularity 75 - pos*5` | popularity fallback | **Last-resort (document):** proxy when a provider returns no popularity metric (Deezer albums). |
| `recency 30d / ×1.1` | `applyRecencyBoost` | **Learn-later / reconsider:** real signal, tuned weight → defer or justify. |
| `lowRelevanceThreshold = 0.3` | spell-suggest | **Reconsider:** tie to tiers (suggest when top tier is T3/T4). |
| `diversityWindow=10, maxPerArtistInTop=3` | `EnforceDiversity` | **Keep (product rule):** a UX choice, documented as such — not a quality constant. |

The ledger is the R5 deliverable: each entry is resolved during the layer it belongs to, and the
verdict is recorded in code comments + here.

---

## Output Structure

    services/go-api/internal/discovery2/        # provisional name
    ├── service/
    │   ├── search.go            # slim orchestrator: fan-out → merge → rank
    │   ├── intent.go            # Layer 0: authoritative structured intent
    │   ├── merge.go             # Layer 2: identifier → version-category → fuzzy-last-resort
    │   ├── rank.go              # Layer 3: lexicographic relevance tiers
    │   ├── consensus.go         # Stage-3 + MB authority + per-artist cache
    │   ├── coverage.go          # the 3 coverage-gap fallbacks
    │   └── telemetry.go         # emission hooks (re-add of 002 U2)
    ├── ports/                   # reuse discovery/ports where identical
    └── adapters/handler/        # or extend the existing handler with the switch

(Provider adapters and domain value objects are imported from `internal/discovery/`, not duplicated.)

---

## Implementation Units

> Each unit rebuilds a layer with categorical decisions and is validated on the **top-K eval**
> in isolation. The old package is the *behavioral reference* (what cases exist), not code to
> copy. "Characterize then rebuild," not "port then prune."

### Phase A — New package + the decision core

- **U1. New package skeleton + handler switch (default old). [DONE 2026-06-21]**
  Stood up `internal/discovery2/service/` (orchestrator skeleton — home for U2–U4) and added the
  **search-surface seam** to the existing handler: an optional `newSearchPipeline` interface (nil =
  off) wired via the functional `Option` `WithNewSearchPipeline`, with an `executeSearch` router. The
  seam returns the legacy `*service.SearchOutput`, so the existing DTO mapping serves both pipelines —
  **response parity by construction** (R4). `app.go` is untouched (passes no option ⇒ legacy path
  provably unchanged); cutover wiring is U8. Only the search seam exists; the consensus surfaces get
  their own seams in U5 (YAGNI — no dead switch points). The `discovery2 → discovery/service` coupling
  (for the shared `SearchOutput` shape) is temporary and dissolves at the final rename.
  *Verified:* full suite **1014 pass** (+3 seam tests); build + vet clean. New tests cover both routing
  directions, the new-pipeline error path (→ 500), and a compile-time assertion that the skeleton
  satisfies the seam. `discovery2.Service.Execute` is a documented not-yet-implemented stub
  (unreachable in prod until U8).

- **U2. Layer 2 — merge + entity resolution (categorical cascade). [DONE 2026-06-21]**
  Built `discovery2/service/merge.go`: `Merge(perProvider) []Entity` runs the cascade
  identifier → version-marker categories → fuzzy-last-resort. `parseVersion` decomposes a title
  into `(core, tags)` where tags are categorical (`feat:<artist>`, sequel `n:N`, and qualifier
  categories — remix/live/acoustic/deluxe/remaster/…). **Same core + same tags = same work; same
  core + different tags = different work** → Pattern B dissolved structurally (no similarity number
  can collapse a sequel). The lone surviving threshold is `fuzzyCoreThreshold` (the documented
  last-resort, never applied across a tag difference). `Entity` carries `BestRank` per provider for
  Layer-3 RRF. Reuses `textnorm.NormalizeForMatch` + `legacy.TokenSortRatio` verbatim.
  Constants-ledger entries resolved: `versionSimilarityThreshold=85` → **replaced** (categorical);
  `consensusTitleMatchMinTSR` deferred to U5.
  *Verified:* 32 discovery2 tests pass (build + vet + gofmt clean). Covers each cascade rung, the
  Pattern-B set (sequel/remix/feat/live kept separate), cross-provider same-work merge, fuzzy typo
  merge, same-title-different-artist separation, artist name merge (incl. "Blink-182" ≠ "Blink"),
  and `BestRank` min-across-providers. (Live merge-sensitive eval slice runs at U8 against real
  providers.)

- **U3. Layer 3 — lexicographic relevance tiers. [DONE 2026-06-21]**
  Built `discovery2/service/intent.go` (Layer 0 `Intent` contract + `BuildIntent`) and `rank.go`
  (`Rank(entities, queryNorm, intent)`). Tiers are categorical: **T1** exact title + artist
  satisfied + kind matches intent (or none intended); **T2** exact title, satisfied artist, but a
  different kind than intended; **T3** partial; **T4** weak. Sort is lexicographic by tier, then
  popularity, then multi-source, then RRF — **a lower tier can never outrank a higher one**. Layer 0
  inference: artist+title query ⇒ intended kind = track (safe — if no track matches, the album is
  still the top non-T1 tier), which structurally seats the exact track at T1 and the same-named
  album at T2 (Pattern A). Eligibility gates (shares-query-word, browseable-source) carried forward.
  Constants-ledger entries resolved: `roundBand` 0.05 → **removed**; `popularityDominance*` →
  **removed** (cross-kind order is now structural); `intentBoost` → **replaced** (intent selects the
  tier); `rrfK=60` → **kept** (within-tier tiebreak).
  *Verified:* 38 discovery2 tests pass (build + vet + gofmt clean). Headline test: Pattern A —
  album with popularity 99 vs track 70, track still ranks #1 (tier beats popularity), album directly
  below. Plus bare-query popularity-within-tier, exact/partial/weak ordering, both eligibility gates,
  and the multi-source within-tier tiebreak. (Full canonical suite + the 17-case slice run live at U8.)

### Phase B — Coverage

- **U4. Layer 1 fan-out (reuse adapters). [DONE 2026-06-21]**
  `discovery2/service/search.go` `Service.Execute` is now real: Layer 0 intent (reuse
  `legacy.DetectIntent` → `BuildIntent`) → Layer 1 parallel fan-out (per-provider timeout +
  circuit breaker, reuse `legacy.CircuitBreaker`, optional `StructuredSearcher`) → Layer 2 `Merge`
  → Layer 3 `Rank` → `legacy.EnforceDiversity` → limit → `*legacy.SearchOutput` + best-effort
  history. `NewService(providers, circuitBreaker, opts...)` with `WithHistoryRepository` /
  `WithVocabularyStore`. Constants ledger: provider timeout 1.5s → **kept** (SLA). Slim by design —
  enrichment (artwork/popularity), correction/suggest, related groups, click-boost are pure reuse
  hooks wired at cutover prep (U8), not rebuild units; telemetry emission is U7.
  *Verified:* 42 discovery2 tests pass (build + vet + gofmt clean). End-to-end via faked providers:
  cross-provider ISRC merge (2 sources) + popularity ranking, partial-on-provider-error, the
  intent→tier Pattern-A path (album pop 99 vs track 40 → track #1), and limit truncation.
  (`-race` unavailable here — no CGO; fan-out reuses the proven mutex+WaitGroup pattern, all shared
  writes guarded, every goroutine joined before return.)

- **U5. Stage-3 consensus + MB authority + per-artist cache. [DONE 2026-06-21]**
  `discovery2/service/consensus.go`: the audited consensus carried forward with two changes — album
  clustering is now **categorical** (`parseVersion` core+tags + fuzzy-core last resort, the same
  cascade as Layer 2) replacing `consensusTitleMatchMinTSR=85` (ledger entry resolved), and a
  **per-artist TTL cache** (`defaultConsensusCacheTTL=6h`, OQ4 policy: short TTL, not event-driven)
  short-circuits the fan-out + MB pass. Provider iteration is slice-ordered (not map-range) so output
  is deterministic. The MB authority pass (confirm / contamination-reject / strong-data authority
  filter) is ported faithfully; its thresholds (mbLookupCap, mbAuthorityMin, mbDiscardMinAlbums) are
  carried-forward audited values, not ranking constants. `consensusTimeout=10s` → **kept** (SLA).
  *Verified:* 47 discovery2 tests pass (build + vet + gofmt clean) — the unit test the old engine
  lacked. Covers confirmed/unconfirmed by provider count, deluxe-as-separate-release (categorical),
  cache-hit-skips-provider-calls, determinism across 5 runs, and MB confirm + contamination-reject.

- **U6. Close the three coverage gaps. [DONE 2026-06-21]**
  **YT Music 0-results (Pattern C) — root cause found by reading the `raitonoberu/ytmusic` source,
  not guessed:** the library routes official songs (`MUSIC_VIDEO_TYPE_ATV`) to `result.Tracks` but
  `OMV`/`UGC` music videos to `result.Videos`; obscure/underground recordings (and top-result cards)
  land in `.Videos`, which the adapter dropped → the exact track never entered the candidate set, so
  the ranker substituted the artist's hit. Fix: map `.Videos` as tracks in the shared adapter
  (`ytmusic.go`) — additive, and the categorical merge dedups any video duplicating an official
  track. **Structural fallbacks:** `discovery2/service/coverage.go` `FirstNonEmptyTracks` — the
  long-tail album-track and underground top-track fallback (first provider with a non-empty list
  wins; errors/empties fall through).
  *Verified:* 137 tests pass across discovery2 + providers (build + vet + gofmt clean). New:
  `mapYTMusicVideo` mapper test (track shape, source, thumbnail, duration) and 6 `FirstNonEmptyTracks`
  cases (fallback, short-circuit, error-fallthrough, all-empty, last-error, ctx-cancel). **Live
  confirmation that Pattern-C tracks now appear, and signal-B improvement on underground artists,
  run at U8 against real providers** (YT Music can't be exercised offline). Fallback wiring into the
  album/artist services is U8.

### Phase C — Cutover (gated on the 002 baseline)

- **U7. Re-add telemetry emission. [DONE 2026-06-21]**
  `discovery2/service/telemetry.go` `emitSearchEvent` mirrors the legacy `search_performed` envelope
  (`domain.InteractionEvent`: result_count, zero_result, top-10) **stamped `pipeline_version="v2"`**
  so ML training data stays attributable across the cutover. Async best-effort: `WithoutCancel` +
  3s timeout (outlives request cancellation), panic-recovered, failures logged not surfaced, tracked
  by an `emitWg` with `WaitForEmit()` for graceful shutdown / deterministic tests. Wired via
  `WithEventStore` and called at the end of `Execute`.
  *Verified:* 56 discovery2 tests pass (build + vet + gofmt clean). Asserts the v2-stamped envelope
  (type, pipeline_version, result_count, zero_result, top), that an append error neither surfaces in
  `Execute` nor panics, and that a nil event store is a no-op.

- **U8. Per-surface cutover (gated, old retained).** For each surface, run the top-K eval +
  coverage signals on the new path; flip only when new ≥ old at the chosen K with no coverage
  regression. Old stays; rollback = flip back. *Verify:* each flipped surface meets-or-beats
  baseline; rollback is instant and lossless.

---

## System-Wide Impact

- **Interaction graph:** handler gains a per-surface switch; two pipelines coexist behind it.
- **Error propagation:** partial-result + per-provider-status behavior preserved; telemetry
  failures logged only.
- **State lifecycle:** per-artist consensus cache adds an invalidation concern (TTL; origin OQ4).
- **API parity:** response shapes identical old vs new (switch invisible to clients) — shared
  response-contract tests.
- **Integration gate:** the top-K eval (002) is the cross-cutting gate; U8 flips depend on it.
- **Unchanged invariants:** wire contracts, provider adapters, domain value objects.

---

## Risks & Dependencies

| Risk | Mitigation |
|------|------------|
| Removing a constant that was load-bearing for a query absent from the eval | The eval is library-derived + diverse + top-K + coverage-backed; residual risk monitored post-flip via signals + abandoned-search telemetry. |
| A categorical rule (version markers) misses a real-world title format | Version-marker parsing is itself eval-gated; unmatched formats fall through to the last-resort fuzzy rung, not silently merged. |
| Cutover before a baseline exists | Hard prerequisite; U8 blocked until the top-K baseline is recorded. |
| Response-shape drift old vs new | Shared response-contract tests; switch invisible by construction. |
| Per-artist cache staleness (missed new release) | TTL + search path still surfaces new releases; policy at implementation (OQ4). |
| Two pipelines coexisting indefinitely | Per-surface flags + finish-each-surface discipline; U8 tracked to completion. |

---

## Sources & References

- **Origin / blueprint:** [docs/brainstorms/2026-06-20-discovery-rebuild-architecture.md](docs/brainstorms/2026-06-20-discovery-rebuild-architecture.md) — §4, §5, §9, §12, §13.
- **Prerequisite (complete):** [docs/plans/2026-06-20-002-feat-discovery-telemetry-eval-step-zero-plan.md](docs/plans/2026-06-20-002-feat-discovery-telemetry-eval-step-zero-plan.md)
- Current pipeline (behavioral reference): `services/go-api/internal/discovery/service/search_music.go`, `dedup.go`, `consensus.go`, `popularity.go`
- Handler/switch point: `services/go-api/internal/discovery/adapters/handler/discovery_handler.go`
- DI: `services/go-api/internal/app/app.go`
- Baseline run: `discoveryeval --mode eval --random` (full prod catalog, 2026-06-20).

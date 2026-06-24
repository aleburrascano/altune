---
title: "test: Discovery eval-harness quality program"
type: test
status: code-complete (unverified against live data)
date: 2026-06-24
origin: grilling session 2026-06-24 (see memory: discovery-eval-harness-program)
---

> **Build status (2026-06-24):** All phases implemented. `go build ./...`,
> `go vet`, and the full suite (1107 tests / 36 packages, incl. new substrate +
> 4 new-harness test files) pass. NOT yet run against live data — `baselines.json`
> is intentionally absent until an operator runs `-update-baselines -noise-runs 3`
> with DB/Redis/providers, at which point every gate goes from "recorded" to live.
> Race detector not run here (no cgo toolchain); harnesses reuse the existing
> mutex-guarded errgroup pattern. New modes on `cmd/discoveryeval`:
> `merge`, `correction`, `diversity`, `health`; `consensus` gained a no-`-query`
> corpus-completeness path. Substrate: `eval_baseline.go`, `eval_failure.go`,
> `eval_adapters.go` + `cmd/discoveryeval/harness.go`.

> **Live test against cloned prod data (2026-06-24):** Cloned prod (1897 tracks,
> 2118 history, 787 clicks; `discovery_events` empty) into the dev Docker Postgres.
> Ran all 8 modes; baselines established (`-noise-runs 3`), re-run gates held green
> within margin, and a **seeded regression tripped → exit 2** (machinery proven
> end-to-end). Sampled (`-limit` 100/300/40 on the dev clone), so the numbers are
> not production baselines. Results (sampled): correction recall **0.935** /
> precision **1.0** (slices show single-token terms are the hard case ✓);
> signal-b mean-gap **0.666** (margin absorbed a 0.684 re-run ✓); health fill-rate
> **94.9%**, bridge-hit **0.0%**, latency p50 **5.3s**/p95 **9.2s**; consensus
> confirmed-fraction **35.8%**.
>
> **Findings (only real data surfaced these):**
> 1. **`merge.collapse_rate` is not a valid metric as built** — collapse = 5%
>    because SoundCloud returns 10–20 distinct re-uploads/edits per track that
>    correctly do NOT merge; the "all variants → one entity" assumption is wrong
>    for track search. `over_merge_rate` (≈0) is sound. **Action: redefine collapse
>    (e.g. cross-provider identity-confirmed dups only, or best-match-per-provider)
>    before relying on it.**
> 2. **`eval` (1.0/1.0) and `diversity` (cost 0) are under-stressed** by the exact
>    `"artist title"` corpus — they don't exercise the single-word/ambiguous hard
>    case. **Action: add a single-token query corpus** (e.g. bare titles) to make
>    these discriminate.
> 3. **Identity-bridge hit-rate 0%** — the cross-provider bridge never fired (dev
>    enrichment cache cold; no `xref` stamping). Real in dev; verify on a warm prod
>    cache.
> 4. `baselines.json` here is **sampled-dev, margin-0 on easy metrics — do NOT
>    commit as production gates**; regenerate full-corpus after fix #1.

> **"Make every verdict excellent" follow-up (2026-06-24):** Lifted the three weak
> harnesses.
> - **merge — fixed, now excellent.** `collapse_rate` (the invalid 5% metric)
>   replaced by `under_merge_rate` = *provable* duplicates left unmerged (shared
>   ISRC/MBID, or identical canonical title+artist that merge.go's own contract
>   must collapse). Live re-measure: **under-merge 0.00%, over-merge 0.00%** —
>   merge collapses everything it provably can; SoundCloud's many distinct
>   re-uploads correctly don't count.
> - **eval + diversity — now discriminate.** Added `-corpus hard` (single-token
>   titles, title-only queries — the ambiguous case). eval drops from 100% (exact)
>   to **top-1 59.5% / top-3 78.4%** over 185 single-token queries. The harness is
>   now a real measurement.
> - **Scores — diagnosed, NOT hacked.** The 40 hard misses split ~50/50:
>   - **Type 2 (~50%, ceiling):** a genuinely more-famous track of the same bare
>     title wins ("Hello"→Adele, "VOGUE"→Madonna). Defensible; discovery isn't
>     personalized, so the owned niche track legitimately isn't top-3. The
>     bare-title corpus tests *beyond* the product promise ("find your track",
>     which the **exact corpus already nails at 100%**).
>   - **Type 1 (~50%, fixable):** an obscure same-named **artist** outranks the
>     owned **track**. Root cause (query dumps): **popularity is Deezer-only**, so a
>     track whose sources are SoundCloud/iTunes/Last.fm scores pop=0 and loses to
>     any same-named artist that happens to be on Deezer. The principled fix is
>     **cross-provider popularity** (SoundCloud play counts, Last.fm listeners,
>     normalized) — substantial, regression-prone ranking work. Deliberately left
>     as a **scoped, gated follow-up**, not slipped into a test session: the
>     hard-corpus eval (gate) + exact-corpus eval (regression guard) now make it
>     safe to attempt. (Per this package's CLAUDE.md: no hardcoded workarounds,
>     fix the algorithm, measure against the top-K eval.)

# test: Discovery eval-harness quality program

## Summary

Discovery quality is measured by **eval harnesses** — system-level information-retrieval
metrics computed over a real corpus — not by unit tests. Two exist today: **ranking**
(`library_eval`, Top1/TopK against the user's own library) and **coverage** (`signal-a`
zero-result gaps, `signal-b` cross-provider imbalance). This program (a) catalogs every
quality the pipeline owns, (b) builds harnesses for the untested ones, and (c) lifts
**all** harnesses — existing and new — to a single adequacy bar so each one produces a
trackable, gated, diagnosable number. Equalized on **rigor, not effort**. All harnesses
are `-mode` modes on the existing `cmd/discoveryeval` tool (live providers, in-process,
nightly / on-demand, never per-commit).

---

## Problem Frame

"Ranking" and "coverage" are the only two discovery qualities with eval harnesses. The
pipeline does far more — entity resolution, query correction, list reshaping, enrichment,
degradation — and each owns a distinct quality that is currently covered only by
example-based unit tests, which answer "does this function work" but never "is the system
*good* at this, and is it getting worse." Worse, the two harnesses that exist only *print*
a number: no committed baseline, no threshold, so a regression is invisible. The ask:
catalog the full metric family, build the missing harnesses, and retrofit the existing
ones, all to one bar that makes every number gate and diagnose.

---

## Metric family (tiered)

Tiered because the metrics are not equal-value. Core metrics change result correctness and
ordering; health metrics are operational and do not.

| Tier | Metric | Stage | Oracle | Status |
|---|---|---|---|---|
| Core (gated) | Ranking — Top1/TopK | Rank | library-as-truth | exists → retrofit |
| Core (gated) | Coverage — gaps + provider imbalance | fanOut / correction | union, correctability | exists → retrofit |
| Core (gated) | **Merge precision / recall** | Merge | **library-as-truth** (id-agreement secondary) | new |
| Core (gated) | **Correction accuracy** | tryCorrection | synthetic perturbation (offline, deterministic) | new |
| Core (gated) | **Diversity / reshaping cost** | EnforceDiversity, CollapseArtistDuplicates | **differential on library oracle** | new |
| Health (report-only) | Enrichment fill-rate | enrich | self-stat | new |
| Health (report-only) | Identity-bridge hit-rate | stampIdentities | self-stat | new |
| Health (report-only) | Latency p50/p95 | whole path | self-stat | new |
| Health (report-only) | Consensus completeness | consensus (detail) | union | exists → retrofit (light) |

---

## Oracles (cheap, blind-spots documented)

The cheap-oracle philosophy is accepted: synthetic / real-data / self-referential oracles
over hand-labeled golden sets (which are small, rot fast, and encode our judgment rather
than reality). Each oracle's blind spot is documented inline; where a blind spot is
genuinely dangerous it gets a small targeted unit test, not a golden corpus.

- **Merge → library-as-truth (primary).** For each owned track, all provider variants must
  collapse into exactly **one** entity (recall + collapse); two *distinct* owned tracks
  must **not** merge (precision). Reuses the matcher `library_eval` already trusts.
  - **Identifier-agreement → secondary cross-check only.** Only MusicBrainz (mbid+isrc) and
    Deezer (isrc) carry identifiers; iTunes / SoundCloud / YouTube / YT Music / Genius carry
    none. So an id-agreement oracle can only label the well-catalogued MB↔Deezer mainstream
    overlap — **blind** to the niche / single-word cases that are the actual risk
    ([[feedback-discovery-testing-gaps]]). Kept only to catch mainstream regressions cheaply.
- **Correction → synthetic perturbation.** Take real vocab/library terms, inject
  keyboard-adjacent typos + transpositions, expect correction back; feed known-good terms,
  expect **no** correction. Offline, deterministic, large, no live providers, no noise
  margin. Blind spot: synthetic typo distribution ≠ real user typos.
- **Diversity → differential on the library oracle.** Diversity has no ground truth for "the
  right amount of variety," so do not measure it standalone. Run `library_eval` with the
  reshaping rule **on vs off**; the metric is **correct results lost to the rule** (entities
  that pass without it but fail with it). Gate that *cost*; print the *benefit* (artist
  concentration drop) **un-gated** — it is product policy, not correctness. Same technique
  covers the whole reshaping tier (EnforceDiversity, CollapseArtistDuplicates).

---

## Adequacy bar (every harness — existing and new)

1. **Real oracle**, blind spot documented inline.
2. **Baseline = freshly *measured* current value**, committed to `cmd/discoveryeval/baselines.json`.
   Never an aspirational target.
3. **Gate = relative drop below baseline.** **No absolute floor** — a hand-picked floor
   (`≥ 0.85`) is exactly the query-fit magic constant `search.go`'s doctrine bans. The
   drop-margin is **empirical**: run each harness 3× back-to-back once, observe the spread,
   set the margin **wider than measured live-provider noise**. Regressions smaller than
   noise are invisible — the documented tax for a real, live oracle. If a metric's noise is
   so wide it gates nothing, make *that* harness deterministic (recorded corpus), case-by-case.
4. **Attributed failure log is the diagnostic artifact.** Every miss is logged with its full
   cheap attribute bag (token count, source/provider list, what-ranked-#1, kind, script,
   has-identifier, raw title). Failure modes are an **output read at investigation time**,
   not a taxonomy maintained up front — re-groupable by any axis, including ones unanticipated
   today. The four mechanical slices (token-count / popularity / script / has-id) are
   **disposable default sugar** printed on top; deletable without losing power.
   `library_eval.FailuresByTopKind` is the seed of this — generalize it.
5. **Runs nightly / on-demand, red exit on regression — never a per-commit gate.** False reds
   are how the whole thing gets deleted (see [[feedback-hooks-pain]]).

Auditing the existing four against this bar: they pass 1–2, **fail 3–4** (no committed
baseline, no threshold; coverage signals have diagnostics but no baseline). "Test the tested
ones better" = retrofit baselines + thresholds + attributed-failure-log onto ranking and
coverage.

**Health gauges are report-only — not thresholded.** Fill-rate, bridge-hit, latency fluctuate
with provider uptime; gating them flaps. Tracked in `baselines.json` for visibility only. One
graduates to gated *only* if it proves it predicts user-visible breakage.

---

## Phases

### Phase 0 — Retrofit the existing four + build the shared substrate

Build the common substrate *while* retrofitting, against code that already works:
- `baselines.json` format + load/compare.
- Attributed-failure-log record + JSON emission (generalize `FailuresByTopKind`).
- Noise-margin ritual (run 3×, record spread, derive margin).
- Four-slice default report view.

Apply to `eval`, `signal-a`, `signal-b`, `consensus`. Measure current baselines + run-to-run
noise so thresholds are empirical from day one.

- **Verify:** each of the four emits a committed baseline + attributed failure log; a seeded
  regression flips the exit code; a no-op re-run stays green within the noise margin.

### Phase 1 — Merge (library-as-truth)

`-mode merge`: recall+collapse (owned-track variants → one entity) and precision (distinct
owned tracks not merged). Identifier-agreement secondary cross-check. Reuses the matcher.

- **Verify:** baseline measured + committed; a deliberately broken merge (e.g. drop the
  exact-title fallback) drops recall below margin and the failure log shows the un-collapsed
  variants.

### Phase 2 — Correction + Diversity

- `-mode correction` (deterministic, offline): synthetic typo recall + known-good precision.
- `-mode diversity`: differential `library_eval` (rule on vs off) for correct-answers-lost;
  needs `rankPipeline` reshaping steps toggleable (they are discrete steps already).

- **Verify (correction):** runs with no live providers; injected typo corpus corrects above
  baseline; known-good corpus triggers zero corrections.
- **Verify (diversity):** rule-off vs rule-on run produces a non-negative cost number; a
  forced over-aggressive cap raises the cost above margin.

### Phase 3 — Health gauges (ungated)

`enrichment fill-rate`, `identity-bridge hit-rate`, `latency p50/p95` as report-only modes /
columns. Tracked in `baselines.json`, no threshold.

- **Verify:** each prints + records its number; none can flip the exit code.

---

## Non-goals / explicitly deferred

- No per-commit CI gating. No blocking merges on eval results.
- No hand-labeled golden sets as a primary oracle.
- No hand-maintained failure-mode taxonomy.
- No absolute thresholds / hand-picked floors.
- Health gauges do not gate (until one earns it).

---

## See also

- `cmd/discoveryeval` — host tool; `internal/discovery/service/library_eval.go`,
  `coverage_signal_a.go`, `coverage_signal_b.go`, `consensus.go` — existing harnesses.
- `internal/discovery/service/search.go` — the zero-magic-constant doctrine the threshold
  rule inherits.
- Memory: `discovery-eval-harness-program`, `feedback-discovery-testing-gaps`,
  `multi-user-music-diversity`, `feedback-hooks-pain`.

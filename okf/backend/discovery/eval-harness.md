---
type: Subsystem
title: Discovery eval harness
description: The offline discoveryeval CLI and its baseline/gate substrate that measure ranking, merge, diversity, coverage, and behavioral-replay quality against committed baselines, nightly rather than per-commit.
resource: services/go-api/cmd/discoveryeval/, services/go-api/internal/discovery/service/eval/
tags: [discovery, eval, regression-gate, behavioral-corpus, replay, subsystem]
verified_commit: c324e0716c50cc6d5e3d7a5255ac9f7552bc0df1
---

`cmd/discoveryeval` (`main.go`) runs the real search pipeline in-process (`app.BuildSearchService`) against cloned prod data, never per-commit (see [ci-cd-pipeline](../../playbooks/ci-cd-pipeline.md) for the narrower gated subset that does run per-commit). Modes: `eval` (library "artist title → top-K", gated top1/topk), `merge` (under/over-merge), `correction` (synthetic-typo precision/recall), `diversity` (reshaping cost), `signal-a`/`signal-b` (telemetry coverage gaps / cross-provider imbalance, gated), `health`/`consensus` (report-only gauges, never gated), `artwork`, `artist-intent`, and `corpus-build`. Eval searches use a nil event store so synthetic runs never pollute telemetry.

Every gated mode flows through one spine, `runHarness` (`harness.go`): run once → write JSON → render the human report → gate headline metrics against `cmd/discoveryeval/baselines.json` → print failure slices → exit 2 (`errRegressed`) on regression. `--update-baselines -noise-runs 3` is the explicit re-baseline path: it runs the harness N times, sets the baseline to the mean and the margin to `MeasureNoise` (peak-to-peak spread × 1.5 headroom) — a hand-picked floor is explicitly banned (`eval_baseline.go`); gates are relative drops below a measured baseline, direction-aware via `NamedMetric.HigherIsBetter`. `Baselines.Gate` reports `Missing` (never a regression) until a baseline is first committed.

`service/eval/` holds the per-mode logic: `merge_eval.go`, `diversity_eval.go`, `health_eval.go`, `correction_eval.go`, `coverage_signal_a/b.go`, `library_eval.go`, `artist_intent_eval.go`. Two files close the behavioral loop: `behavioral_corpus.go`'s `CorpusBuilder` mines `ports.BehavioralLabelStore.BehavioralLabels` (search→completed/library_add ⇒ +1, wrong_album ⇒ hard −1 — see [telemetry](telemetry.md)) into a self-growing `BehavioralCorpus` materialized to JSON (`corpus-build` mode, nightly job) — it sharpens because labels are about the user's own catalog, the answer to why global popularity regressed this niche library. `replay.go`'s `ReplayCorpus` scores a candidate ranker's `CandidateRanking` (query → ordered signatures) against that corpus offline — MRR over positives, top-K leakage over negatives — collapsing an experiment from weeks-shipped-dark to a same-day run.

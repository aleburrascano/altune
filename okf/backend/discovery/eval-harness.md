---
type: Subsystem
title: Discovery eval harness
description: The offline discoveryeval CLI and its baseline/gate substrate that measure ranking, merge, diversity, coverage, and behavioral-replay quality against committed baselines, nightly rather than per-commit.
resource: services/go-api/cmd/discoveryeval/, services/go-api/internal/discovery/service/eval/
tags: [discovery, eval, regression-gate, behavioral-corpus, replay, subsystem]
verified_commit: dc56d3381f7ae1f20f1b124c530bf848316d21ab
---

`cmd/discoveryeval` (`main.go` plus its split-out files) runs the real search pipeline in-process (`app.BuildSearchService`) against cloned prod data, never per-commit (see [ci-cd-pipeline](../../playbooks/ci-cd-pipeline.md) for the narrower gated subset that does run per-commit). `main.go` holds the CLI entrypoint, the per-mode `run*` handlers, and the small adapter/helper types they use (`variantSearchAdapter`, `searchAdapter`, `corpusEntities`, `filterRecognized`, `buildEvalSearcher`, `evalQuery`, and the consensus tally helpers `tallyConsensus`/`pooledPct`/`buildArtistConsensus`/`consensusSingle`/`consensusCompleteness` — `buildArtistConsensus` passes the resolved Deezer `(provider, artistID)` seed into `BuildConsensus`, matching the service's seeded signature); `dbload.go` holds the cross-user library loaders (`loadLibraryEntities`, `loadLibraryTerms`, `loadDistinctArtists`), the latter two now sharing a `queryStrings` scan-and-LIMIT helper (previously each had its own duplicate scan loop, and `loadLibraryTerms` built its LIMIT via a subquery wrapper — `SELECT term FROM (...) t LIMIT %d` — where `queryStrings` now appends `LIMIT` directly to the UNION query text; same result set, different SQL shape); `render.go` holds `maybeWriteJSON` and the per-mode human-report renderers (`renderEval`, `renderArtistIntent`, `renderMerge`, `renderDiversity`, `renderHealth`, `renderConsensus`, `renderCorrection`, `renderSignalA`, `renderSignalB`). This is a file split for navigability, not a behavior change. Modes: `eval` (library "artist title → top-K", gated top1/topk), `merge` (under/over-merge), `correction` (synthetic-typo precision/recall), `diversity` (reshaping cost), `signal-a`/`signal-b` (telemetry coverage gaps / cross-provider imbalance, gated), `health`/`consensus` (report-only gauges, never gated), `artwork`, `artist-intent`, `corpus-build`, and `detail` (the artist detail/discography path, not search ranking — see below). Eval searches use a nil event store so synthetic runs never pollute telemetry. Process setup mirrors `cmd/api` — env config then `logging.Setup(logLevel, development)` — so eval logs use the same handler stack as production.

Every gated mode flows through one spine, `runHarness` (`harness.go`): run once → write JSON → render the human report → gate headline metrics against `cmd/discoveryeval/baselines.json` → print failure slices → exit 2 (`errRegressed`) on regression. `--update-baselines -noise-runs 3` is the explicit re-baseline path: it runs the harness N times, sets the baseline to the mean and the margin to `MeasureNoise` (peak-to-peak spread × 1.5 headroom) — a hand-picked floor is explicitly banned (`eval_baseline.go`); gates are relative drops below a measured baseline, direction-aware via `NamedMetric.HigherIsBetter`. `Baselines.Gate` reports `Missing` (never a regression) until a baseline is first committed.

`service/eval/` holds the per-mode logic: `merge_eval.go`, `diversity_eval.go`, `health_eval.go`, `correction_eval.go`, `coverage_signal_a/b.go`, `library_eval.go`, `artist_intent_eval.go`, `detail_eval.go`. `CoverageSignalAService` mines three query-grain gaps off `ports.EventQuery` into `CoverageReportA`: `Strong` (zero-result, not a correctable typo), `Weak` (results shown, no click), and `Abandoned` (no click, reformulated within 60s — `EventQuery.AbandonedSearches`, a Joachims query-chain dissatisfaction signal, see [telemetry](telemetry.md)). All three are report-only until `signal_a.strong_gaps`/`signal_a.abandoned_gaps` get a committed baseline (`--update-baselines`); `Weak` never gates (a hint, not a failure). Two files close the behavioral loop: `behavioral_corpus.go`'s `CorpusBuilder` mines `ports.BehavioralLabelStore.BehavioralLabels` (search→completed/library_add ⇒ +1, wrong_album ⇒ hard −1 — see [telemetry](telemetry.md)) into a self-growing `BehavioralCorpus` materialized to JSON (`corpus-build` mode, nightly job) — it sharpens because labels are about the user's own catalog, the answer to why global popularity regressed this niche library. `replay.go`'s `ReplayCorpus` scores a candidate ranker's `CandidateRanking` (query → ordered signatures) against that corpus offline — MRR over positives, top-K leakage over negatives — collapsing an experiment from weeks-shipped-dark to a same-day run.

The `detail` mode (`detail_eval.go`) gates the OTHER discovery pipeline — the artist detail/discography path ([artist-detail](artist-detail.md)), not search ranking. `RunDetailEval` runs `GetArtistContentService` in-process against LIVE providers but over a SEEDED in-memory identity store built from the committed `cmd/discoveryeval/detail_goldens.json` (`detail.go` + `app.BuildArtistContentService`), (the seeded store satisfies the full `IdentityStore` port, including a no-op `Invalidate`), so a golden can carry a deliberately FRACTURED identity — a wrong streaming edge fusing two same-name artists, the "Che" bug — and the harness asserts the read-time guards (MB anchor for albums, cohesion for top-tracks) drop every contaminated item. It scores `detail.contamination` (gated at 0), `detail.album_recall`/`detail.track_recall`, and `detail.metadata_coverage`; a golden's `forbidden_sources`/`forbidden_titles` are the other artist's fingerprints, `expected_*` its real catalogue. Unlike the library modes it needs no DB (the seeded store replaces it), so it runs anywhere the config validates.

The harness's own math is unit-tested (2026-07-24 QA sweep, package at ~94% coverage): the gating comparisons (`GateAll`/`AnyRegressed`, margin boundaries), baseline round-trips, every `HarnessReport` adapter's metrics/failure tagging, the artist-intent outcome matrix, and the corpus/goldens load error paths — so a regression in the regression-gate itself fails deterministic CI, not a nightly run.

A gate is a relative drop below a committed baseline, never an absolute floor. A metric with no baseline is never a regression (the first run establishes it), baselines move only on an explicit operator `--update-baselines`, and a regression strictly inside the noise margin is invisible by design. Diversity's benefit metric is report-only and never gated — you gate the cost of a policy, not the policy itself. Entities the search never finds are a coverage miss, not a merge miss.

## Why the corpus is frozen

A rate is only comparable across runs if its denominator is. The corpus was originally rebuilt live from `SELECT DISTINCT title, artist FROM tracks` on every run, so it grew with the library — and the first scheduled nightly (2026-07-25) failed for exactly that reason: the eval baselines had been measured over 1877 entities (1849 top-1, 1868 top-3) and the run evaluated 1942. `eval.top1_rate` came out 1.6e-5 below its baseline, identical at four decimals, and with `margin: 0` that counted as a regression. Nothing about ranking had changed; 65 tracks had been added.

So `corpus.go` reads a committed snapshot (`corpus-library.json`) instead: `resolveEntities` / `resolveArtists` / `resolveTerms` fall back to the `dbload.go` loaders only when `-corpus-file` is unset, so ad-hoc local runs are unchanged while the nightly is pinned. `LibraryCorpus` (`service/eval/library_corpus.go`) sorts on construction and derives the artist and term lists from the same entity pairs, so one file feeds `eval`, `merge`, `diversity`, `artist-intent`, `signal-b` and `correction` and they cannot disagree about what the library is. A frozen-corpus delta therefore means the code or a provider changed — the only two things left that can move.

`-random` with `-corpus-file` is an error rather than a silent override: a frozen corpus exists to be deterministic, and sampling it randomly defeats the point while looking like it worked.

Refreshing the snapshot moves the denominator again, which is why it is a dispatch-triggered PR (`discovery-eval-corpus-refresh.yml`) and not an automatic step. Every corpus-derived baseline must be re-measured after it merges, or the next nightly reports the size change as a regression.

## Why margins are measured on the runner

`-noise-runs N` sets each margin to the run-to-run spread, and for the live-provider modes that spread is mostly provider behaviour: MusicBrainz pacing, circuit-breaker trips, rate-limited responses. Which of those you observe depends on which IP you ask from. A margin measured on a laptop describes the laptop's network and then gets enforced against GitHub runner IPs, so re-baselining is a cloud workflow (`discovery-eval-rebaseline.yml`) rather than a local command. The PR it opens is what keeps "never re-baseline implicitly" true.

Provider noise is also why a regressed mode re-runs once before the nightly calls it red. A single red result is not evidence — `detail`'s contamination guard is fail-open on a MusicBrainz timeout and has reported 0 then 70 on the same commit ~30s apart. Two consecutive regressions is signal.

## Why the nightly fans out

The modes run as an eight-way matrix with `fail-fast: false`, one job per mode. Before that they were sequential steps in a single job, so the first mode to exit 2 skipped every mode after it: the 2026-07-25 run learned nothing about `artist-intent`, `diversity`, `signal-a` or `signal-b` because `eval` failed first. A nightly that stops at the first regression cannot answer whether discovery improved overall.

Concurrency is capped by provider quota scope, not by runner availability. `liveTransport`'s limiter is per-process (see [app-wiring](../app-wiring.md)), so N concurrent jobs issue N times the request rate. MusicBrainz and iTunes are IP-scoped and unaffected by a second runner; Last.fm, Discogs and Genius are key-scoped, and the CI credentials are one key each. So the modes that spend key quota — `eval`, `merge`, `diversity`, `artist-intent`, and `signal-b`, which fans out over `BuildConsensusProviders` — run one at a time, while `detail` (MusicBrainz only), `correction` (offline) and `signal-a` (telemetry) run alongside them.

`correction` hard-requires a populated vocabulary store, and the VM's Redis is unreachable from Actions by design. Rather than leave the mode permanently ungated, `correction-seed` (`seed.go`) `BulkAdd`s the corpus's artists and titles into a CI Redis service container. That vocabulary is seeded, not learned from real search traffic, so its recall and precision are not comparable to a run against prod's store — the mode carries its own baseline measured the same seeded way.

## Where the trend lives

`-metrics path` writes a flat, mode-agnostic `{mode, generated_at, metrics[]}` file per run, and `-mode report` (`report.go`) globs those into one gate table: current, baseline, margin, signed delta, verdict, rendered to the job summary. It writes every metric into the existing `discovery_metrics` table (see [discovery-metrics-table](../../data/discovery-metrics-table.md)) keyed `(as_of, metric)`, so eval history outlives the 30-day artifact retention and sits alongside CTR and zero-result rate for the admin console to chart. The flat metrics file exists so the aggregator never has to unmarshal `HarnessReport` polymorphically — it does not need to know which mode produced what.

When something regresses, `report` also writes `regressions.txt` naming each metric and its delta, which is what the push notification carries. The alert previously said only that "a gated mode fell below baselines".

## Report field glossary

The report structs in `service/eval/` are the JSON wire contract for each harness. Their fields:

- `Baseline{Value, Margin, HigherIsBetter, Note}` — `Margin` is the empirical noise band, always at least the observed run-to-run spread. `HigherIsBetter` is true for rates, false for cost/latency/gap counts. `GateResult.Threshold` is the value `current` must stay on the safe side of; `Missing` means no committed baseline and is informational only.
- **Library eval** — `Evaluated` = `Total − Skipped`, the rate denominator. `MatchPosition` is 0-based, `-1` when not in top-K. `Top1Passed` = ranked #1; `TopKPassed` includes top-1; `Failed` = not in top-K or no results. `FailuresByTopKind` records what kind ranked #1 on a miss (including `"none"`). `Corpus` is `""` for exact, `"hard"` for title-only ambiguous.
- **Artist-intent eval** — `ArtistPos` / `FirstTrackPos` are 0-based, `-1` if absent. `Buried` is the bug: the artist card is present but a same-name track ranks above it. `BelowK` is present-but-below-K without being track-buried. `Absent` is the recall gap — the card never surfaced. `Corpus` `"hard"` means single-token names.
- **Merge eval** — `ResultsSeen` is the under-merge denominator (all rows), `DistinctSeen` the over-merge denominator. `NoMatch` is a coverage miss, not a merge miss. `UnderMergeIncidents` counts provable duplicates left unmerged; `CleanQueries` are those with zero.
- **Correction eval** — `Terms` is the precision denominator, `TyposTested` the recall denominator. `Corrupted` is a false positive: a valid term the corrector rewrote.
- **Diversity eval** — `LostToReshape` (in top-K unshaped, out reshaped) is the cost and the gated metric; `GainedByReshape` is the reverse, since collapse can promote. `ConcentrationWith`/`Without` are mean top-K Herfindahl — the benefit side, report-only.
- **Coverage signal A** — `Strong` = zero-result and not typos; `Weak` = results shown, no click; `Abandoned` = no click, reformulated within 60s. **Signal B** — `GapPct` = `Missing / Union` in [0,1]; `Unique` counts entities only that provider had.
- **Health** — `FillRate` = with-artwork / results; `BridgeHitRate` = bridged-merges / results.
- **Detail eval** — `Identity` (provider → id) may be deliberately fractured; `ForbiddenSources` / `ForbiddenTitles` must not appear in any result.

## Measured state (2026-06-24, cloned prod data, 1897 tracks)

- Exact `"artist title"` eval — **100% top-3**. This is the real product bar, and it is met.
- Bare single-token title eval — **~81% top-3**. A stress metric with an inherent ambiguity ceiling: a bare title ("Hello", "Scorpion") legitimately surfaces the *famous* track over the user's niche owned one, and discovery is not personalized. Not a bug.
- Merge — **0% under-merge / 0% over-merge**.
- Correction — **93% recall / 100% precision**.

## Why the testing discipline is what it is

Position, not presence: "is the right answer in the top 10" is far too weak to catch ranking regressions, which is why the gate is top-3. A/B must run on an identical deterministic sample (`-limit`, no `-random`) because deltas across random samples are noise — a plausible change once looked "+7pp" and the same-sample A/B revealed it as a regression. That is how the popularity attempt was caught (see [ranking](ranking.md)). Adding a word to a bank or a special-case list to fix a bad query rots immediately; fix the algorithm instead. Each session tends to add ranking layers, so the question to ask of any stage is "if I remove this, do the positioning tests still pass?"

The canonical spot-checks (top-3, blended, tested both blended and filtered by `kinds=album` / `kinds=track`):

```
"Humble"                → top-3 contains the Kendrick track "HUMBLE."
"Scorpion"              → top-3 contains a Drake "Scorpion" result
"Bohemian Rhapsody"     → top-3 contains a Queen result
"Drake" / "Bad Bunny"   → top-3 contains the artist
"Blinding Lights"       → top-3 contains the Weeknd track
"Kendrick Lamar Humble" → top-3 contains "HUMBLE." by "Kendrick Lamar"
```

`track>album>artist` is a held-in-reserve, non-query-fit tiebreak for strict-#1 polish — not the gate.

## The gated spine

Every gated mode goes through `runHarness`, which gives them one identical shape: run once, write JSON, print the mode's human render, gate the headline metrics against `baselines.json`, print the gate block and the attributed-failure slices, and exit 2 on regression (`errRegressed`).

Re-baselining is explicit only. `-update-baselines` runs the harness `noiseRuns` times — the noise ritual — sets each metric's baseline to the mean and its margin to the measured spread, and merges into the existing file leaving other modes untouched. A missing `baselines.json` is not an error: it yields an empty set, so every metric reports `Missing` until it is baselined.

`renderSlices` is the default view over the attributed failure log — total, the four mechanical single-key slices, then the token by popularity joint band where ambiguity tends to surface. It is disposable sugar; the raw log in the JSON output is the real artifact.

## Why record/replay exists

The ranking eval is non-deterministic against live providers: catalog drift and network jitter churn the failure set run to run, so a real ranking regression hides in the noise. Fixtures freeze the provider I/O — record every provider's raw HTTP responses once, then replay them through the real ranking pipeline (`app.BuildSearchServiceWithTransport`, `rankingOnly`) so the same wiring runs deterministically.

Record and replay both use one shared `Service` over a single recorder or replayer. That matters for size and speed: SoundCloud bootstraps its `client_id` once (a ~3 MB JS asset) rather than per query, and YouTube Music's package-global HTTP client is set once, so recording can run at full concurrency. The recorder wraps the live rate-limiting, retrying transport, so a bulk record paces itself inside each provider's limit instead of hammering it into throttling and capturing self-inflicted timeouts. Redis is left nil on both paths so cache state cannot add variance — the frozen provider responses are the only input.

The `Replayer` matches by request identity, so one combined set serves every query and order across files does not matter; `loadAllFixtures` concatenates every `*.json` in the directory, so a sharded corpus still works. `dedupExchanges` keeps one exchange per `(method, URL, request body)`: identical requests carry identical responses, so collapsing is lossless, and it removes duplicate `client_id`-bootstrap fetches from a concurrent first burst. Fixtures are written compact rather than indented, because the corpus file reaches gigabytes where indentation only inflates it. Recording writes one corpus file at the end, so the `\r` progress counter gives no mid-run signal and progress is emitted newline-terminated for a useful log tail.

## Mode notes

`corpusEntities` applies corpus selection: **hard** keeps only single-token titles — the ambiguous case, "Humble", "Scorpion" — and signals title-only. `artist-intent` with `corpus=hard` isolates single-token artist names, the bug's actual home.

`correction` is offline with respect to providers, reading the vocabulary store and the library only; `filterRecognized` keeps only terms the store holds exactly, so a recall miss means the corrector failed rather than that the term was never in vocabulary.

`artwork` runs the real pipeline per distinct library artist and buckets the top artist result by how its image resolved — identity, provider, name or blank — printing aggregate percentages plus the attributed gaps, meaning the name-guesses that are risky for same-name artists and the blanks. It is live, so bound it with `-limit` and `-concurrency`, and flush Redis first for a cold measurement.

`detail` is the offline quality gate for the artist detail path, running the real detail service in-process against live providers but over a seeded in-memory identity store built from the goldens, so a golden can carry a deliberately fractured identity — a wrong streaming edge, the "Che" bug — and the harness verifies the read-time guards drop the contamination. It touches neither the library DB nor Redis; `seededIdentityStore` answers `LookupByProviderID` from that fixed map.

`health` records gauges for visibility and history only on an explicit update, and is never gated. `loadLibraryEntities` is an offline-only cross-context read of the catalog `tracks` table across all users, where random sampling needs a subquery because `DISTINCT` must resolve before `ORDER BY random()`. It is now reached through `resolveEntities`, which prefers a frozen `-corpus-file` when one is given.

`corpus-snapshot` writes that frozen file and refuses an empty result — an empty snapshot would silently pass every gate rather than fail loudly. `report` aggregates and gates; `correction-seed` populates a vocabulary store. Neither `report` nor `corpus-snapshot` runs the search pipeline.

## discoverytrace

`cmd/discoverytrace` is the single-query counterpart to the corpus harnesses: it runs a discovery search behind a recording HTTP transport and dumps the exact payload at each stage — raw provider JSON before parsing, the mapped `[]SearchResult`, and in pipeline mode the merge, rank and reshape stages. The point is watching the data mutate stage by stage rather than confirming a call happened. It is offline and read-only, reusing the exported `Merge` / `RankWith` / `Reshape` without touching the production path.

Providers come from `app.BuildDiscoveryProviders` — the production set over the recording transport — and must never be hand-mirrored locally: a local copy drifted once and SoundCloud silently lost its yt-dlp fallback. `stampIdentities` (the xref bridge) and artwork enrichment are skipped since neither reorders results, so bridge-only merges do not appear in the dumps. Rank runs the same flag-gated experiment stages production applies, except the behavioral stage, which is a live-`Service` snapshot unavailable offline and therefore nil. `printRanked` deliberately shows only rank position, kind, title/subtitle, source count and providers, because order is the signal; the old per-result relevance breakdown was boost-specific debugging and went away with the boost.

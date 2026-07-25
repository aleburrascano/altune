# discoveryeval — offline discovery quality harness — router

Exercises the **real** search pipeline in-process (via `app.BuildSearchService`) and reads discovery's own telemetry. Runs nightly or on demand, **never per-commit**.

Layout:

- `main.go` — mode dispatch and the non-gated diagnostic modes.
- `harness.go` — the shared spine every gated mode flows through, plus baselines file IO and gate/slice rendering.
- `fixtures.go` — record/replay of provider HTTP exchanges for deterministic ranking runs.
- `detail.go` (+ embedded `detail_goldens.json`) — the artist-detail gate and its seeded identity store.
- `artwork.go` — artwork-resolution coverage over the library's distinct artists.
- `dbload.go` — offline reads of the catalog `tracks` table for corpus building.

## Modes

| Mode | What it measures | Gated |
|---|---|---|
| `eval` | ranking — library "artist title → top-K" | yes (`top1`, `topk`) |
| `signal-a` / `signal-b` | per-signal contribution | yes |
| `merge` | entity-resolution merge quality | yes |
| `correction` | query correction recall | yes |
| `diversity` | reshaping cost differential on the library oracle | yes |
| `detail` | artist detail/discography same-name contamination, recall, metadata | yes (contamination `=0`) |
| `artist-intent` | bare-artist-name queries against the artist-card oracle | yes |
| `health` | fill-rate / bridge-hit / latency | **no — report-only** |
| `query` | single-query diagnostic dump, v1 vs v2 title-by-title | no |

## The gated spine

Every gated mode goes through `runHarness`, which gives them one identical shape: run once → write JSON → print the mode's human render → gate the headline metrics against `baselines.json` → print the gate block and the attributed-failure slices → exit 2 on regression (`errRegressed`).

Re-baselining is **explicit only**: `-update-baselines` runs the harness `noiseRuns` times (the noise ritual), sets each metric's baseline to the mean and its margin to the measured spread, and merges into the existing file leaving other modes untouched. A missing `baselines.json` is not an error — it yields an empty set, so every metric reports `Missing` (recorded, never regressed) until it is baselined.

`renderSlices` is the default view over the attributed failure log: total, the four mechanical single-key slices, then the token×popularity joint band where ambiguity tends to surface. It is disposable sugar — the raw log in the JSON output is the real artifact.

## Fixtures: why record/replay exists

The ranking eval is non-deterministic against live providers — catalog drift and network jitter churn the failure set run to run, so a real ranking regression hides in the noise. Fixtures freeze the provider I/O: record every provider's raw HTTP responses once, then replay them through the **real** ranking pipeline (`app.BuildSearchServiceWithTransport`, `rankingOnly`) so the same wiring runs deterministically.

- **Record and replay both use ONE shared `Service`** over a single recorder/replayer. This matters for size and speed: SoundCloud bootstraps its `client_id` once (a ~3 MB JS asset) instead of per query, and YouTube Music's package-global HTTP client is set once — so recording can run at full concurrency.
- The recorder wraps the **live** (rate-limiting, retrying) transport, so a bulk record paces itself inside each provider's limit instead of hammering it into throttling. The captured responses are clean, not self-inflicted timeouts.
- Redis is left **nil** on both paths so cache state cannot add variance — the frozen provider responses are the only input.
- The `Replayer` matches by request identity, so one combined set serves every query and order across files doesn't matter. `loadAllFixtures` concatenates every `*.json` in the dir, so a sharded corpus still works.
- `dedupExchanges` keeps one exchange per `(method, URL, request body)`. Identical requests carry identical responses, so collapsing is lossless, and it removes duplicate `client_id`-bootstrap fetches from a concurrent first burst.
- Fixtures are written **compact, not indented** — the corpus file reaches gigabytes, where indentation only inflates it.
- Recording writes one corpus file at the end, so the `\r` progress counter gives no mid-run signal; progress is emitted newline-terminated so a tail of the log is useful.

## detail mode

The offline quality gate for the artist detail/discography path (`okf/backend/discovery/artist-detail.md`), sibling of the ranking eval. It runs the real detail service in-process against **live** providers but over a **seeded in-memory identity store** built from the goldens — so a golden can carry a deliberately fractured identity (a wrong streaming edge, the "Che" bug) and the harness verifies the read-time guards drop the contamination. It touches neither the library DB nor Redis.

`seededIdentityStore` answers `LookupByProviderID` from that fixed map — the harness's stand-in for the durable store.

## Corpus and mode notes

- `corpusEntities` applies corpus selection; **hard** mode keeps only single-token titles — the ambiguous case ("Humble", "Scorpion") — and signals title-only. `artist-intent` with `corpus=hard` isolates single-token artist names, the bug's actual home.
- `correction` mode is offline with respect to providers: it reads the vocabulary store and the library only. `filterRecognized` keeps only terms the store holds exactly, so a recall miss means the corrector failed rather than that the term was never in vocabulary.
- `artwork` runs the real pipeline per distinct library artist and buckets the top artist result by **how** its image resolved (identity / provider / name / blank), printing aggregate percentages plus the attributed gaps — name-guesses (risky for same-name artists) and blanks. It's live: bound it with `-limit` and `-concurrency`, and flush Redis first for a cold measurement.
- `health` records gauges for visibility/history only on an explicit update; it is never gated.
- `loadLibraryEntities` is an offline-only cross-context read of the catalog `tracks` table across **all** users. Random sampling needs a subquery because `DISTINCT` must resolve before `ORDER BY random()`.
- The single-query `-query` flag belongs to the search diagnostic, except in consensus mode.

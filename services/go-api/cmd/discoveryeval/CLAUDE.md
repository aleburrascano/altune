# discoveryeval — offline discovery quality harness — router

Exercises the real search pipeline in-process (`app.BuildSearchService`) and reads discovery's own telemetry. Nightly or on demand, never per-commit.

Layout:

- `main.go` — mode dispatch and the non-gated diagnostic modes.
- `harness.go` — the shared gated spine, baselines file IO, gate/slice rendering.
- `fixtures.go` — record/replay of provider HTTP exchanges.
- `detail.go` (+ embedded `detail_goldens.json`) — the artist-detail gate and its seeded identity store.
- `artwork.go` — artwork-resolution coverage over the library's distinct artists.
- `dbload.go` — offline reads of the catalog `tracks` table for corpus building.
- `corpus.go` (+ committed `corpus-library.json`) — the frozen-corpus resolvers and the snapshot writer.
- `report.go` — per-run metric files, the cross-mode gate table, the regression digest.
- `seed.go` — vocabulary seeding for a `correction` run with no learned store.

Test files: `fixtures_test.go`, `corpus_test.go`, `report_test.go`.

```bash
cd services/go-api
go run ./cmd/discoveryeval -mode eval                       # ranking, exact corpus (top-3 bar)
go run ./cmd/discoveryeval -mode eval -corpus hard          # bare single-token titles
go run ./cmd/discoveryeval -mode merge                      # under/over-merge
go run ./cmd/discoveryeval -mode correction                 # typo recall/precision (offline)
go run ./cmd/discoveryeval -mode diversity                  # reshaping cost
go run ./cmd/discoveryeval -mode signal-a|signal-b          # coverage gaps / provider imbalance
go run ./cmd/discoveryeval -mode artist-intent              # bare-artist-name queries
go run ./cmd/discoveryeval -mode detail                     # same-name contamination (gated =0)
go run ./cmd/discoveryeval -mode artwork -limit N -random   # artwork coverage
go run ./cmd/discoveryeval -mode health|consensus           # report-only gauges
go run ./cmd/discoveryeval -mode eval -update-baselines -noise-runs 3

# Frozen corpus — what the nightly gates against
go run ./cmd/discoveryeval -mode corpus-snapshot -corpus-file cmd/discoveryeval/corpus-library.json
go run ./cmd/discoveryeval -mode eval -corpus-file cmd/discoveryeval/corpus-library.json
go run ./cmd/discoveryeval -mode correction-seed -corpus-file cmd/discoveryeval/corpus-library.json
go run ./cmd/discoveryeval -mode report -reports ./tmp/reports
```

Flags: `-limit N`, `-concurrency N`, `-top-k 3`, `-query "X"`, `-json path`, `-corpus-file path`, `-metrics path`, `-reports dir`.

## Rules

- Every mode except `health`, `consensus` and `query` is gated — a regression must exit 2.
- Never re-baseline implicitly; only `-update-baselines` may move `baselines.json`.
- Never gate a metric measured on a corpus other than the one its baseline was measured on.
- Never refresh `corpus-library.json` without re-baselining every corpus-derived mode in the same PR chain.
- Never measure a noise margin anywhere but the runner that enforces it.
- Never run two provider-heavy modes concurrently against the same provider credentials.
- Never combine `-random` with `-corpus-file`.
- Never gate a benefit metric — gate the cost of a policy, not the policy.
- Never build more than one `Service` for a record or replay run.
- Never record through anything but the live rate-limiting transport.
- Never wire Redis into a fixture run.
- Never answer an unmatched replay request with an empty result.
- Never write fixtures indented.
- Never run `detail` against the durable identity store — it uses the seeded one.

Why each rule exists, the mode semantics and the report field glossary: `okf/backend/discovery/eval-harness.md`.

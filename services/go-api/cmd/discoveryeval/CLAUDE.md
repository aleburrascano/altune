# discoveryeval — offline discovery quality harness — router

Exercises the real search pipeline in-process (`app.BuildSearchService`) and reads discovery's own telemetry. Nightly or on demand, never per-commit.

Layout:

- `main.go` — mode dispatch and the non-gated diagnostic modes.
- `harness.go` — the shared gated spine, baselines file IO, gate/slice rendering.
- `fixtures.go` — record/replay of provider HTTP exchanges.
- `detail.go` (+ embedded `detail_goldens.json`) — the artist-detail gate and its seeded identity store.
- `artwork.go` — artwork-resolution coverage over the library's distinct artists.
- `dbload.go` — offline reads of the catalog `tracks` table for corpus building.

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
```

Flags: `-limit N`, `-concurrency N`, `-top-k 3`, `-query "X"`, `-json path`.

## Rules

- Every mode except `health`, `consensus` and `query` is gated — a regression must exit 2.
- Never re-baseline implicitly; only `-update-baselines` may move `baselines.json`.
- Never gate a benefit metric — gate the cost of a policy, not the policy.
- Never build more than one `Service` for a record or replay run.
- Never record through anything but the live rate-limiting transport.
- Never wire Redis into a fixture run.
- Never answer an unmatched replay request with an empty result.
- Never write fixtures indented.
- Never run `detail` against the durable identity store — it uses the seeded one.

Why each rule exists, the mode semantics and the report field glossary: `okf/backend/discovery/eval-harness.md`.

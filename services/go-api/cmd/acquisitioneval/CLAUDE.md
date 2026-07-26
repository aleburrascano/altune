# acquisitioneval — router

`main.go` + `baselines.json`. The offline gate for acquisition *selection*: does the pipeline store the right recording? Harness cores live in `internal/acquisition/service/eval/`; goldens are embedded from `internal/acquisition/service/eval/goldens/`.

```bash
go run ./cmd/acquisitioneval                                    # run the embedded suite
go run ./cmd/acquisitioneval -v                                 # keep pipeline logging
go run ./cmd/acquisitioneval -goldens ./path/to/dir             # run an external suite
go run ./cmd/acquisitioneval -baseline ./cmd/acquisitioneval/baselines.json
go run ./cmd/acquisitioneval -baseline ./cmd/acquisitioneval/baselines.json -update-baselines
```

## Rules

- The harness runs the real `CoreSteps` pipeline in-process; never reimplement ranking or the gates here.
- A golden case states per-candidate truth (`correct`, `actual_duration`, `undecodable`, `download_fails`); expectation is derived, never written twice.
- A case with no `correct` candidate must acquire nothing — storing the wrong recording is worse than storing none.
- Re-baselining is explicit (`-update-baselines`); a gated run never rewrites the baseline.
- Every case carries a failure class from `internal/acquisition/ARCHITECTURE.md` §1.

Why the classes exist and what each defends: `internal/acquisition/ARCHITECTURE.md`.

# discoverytrace — router

`main.go` only. Runs one discovery search behind a recording HTTP transport and dumps the exact payload at each pipeline stage.

```bash
go run ./cmd/discoverytrace -query "Ken Carson Olympics"                  # full pipeline
go run ./cmd/discoverytrace -mode single -provider soundcloud -query "…"  # one provider
```

## Rules

- Never hand-mirror the provider list — take it from `app.BuildDiscoveryProviders`.
- Never change the production path from here; it is offline and read-only.
- Never add a per-result score breakdown back to `printRanked` — order is the signal.

Why each rule exists, and what the dumps deliberately omit: `okf/backend/discovery/eval-harness.md`.

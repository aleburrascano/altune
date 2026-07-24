# discoverytrace — router

`main.go` only. Runs a discovery search behind a recording HTTP transport and dumps the exact payload at each stage — raw provider JSON before parsing, the mapped `[]SearchResult`, and in pipeline mode the merge → rank → reshape stages. The point is watching the data mutate stage by stage, not just confirming a call happened.

```bash
go run ./cmd/discoverytrace -query "Ken Carson Olympics"                  # full pipeline
go run ./cmd/discoverytrace -mode single -provider soundcloud -query "…"  # one provider
```

Invariants:

- Offline and read-only: it reuses the exported `Merge` / `RankWith` / `Reshape` and never changes the production path.
- Providers come from `app.BuildDiscoveryProviders` — the production set over the recording transport. **Never hand-mirror the provider list here**: a local copy drifted once and SoundCloud silently lost its yt-dlp fallback.
- `stampIdentities` (the xref bridge) and artwork enrichment are skipped — neither reorders results — so bridge-only merges do not appear in the dumps.
- Rank runs the same flag-gated experiment stages production applies; the behavioral stage is a live-`Service` snapshot and is unavailable offline, so it is nil here.
- `printRanked` deliberately shows only rank position, kind, title/subtitle, source count and providers. Order is the signal. The old per-result relevance breakdown was boost-specific debugging and went away with the boost; ranking is now the parameter-free measure in `internal/discovery/service/rank_relevance.go`.

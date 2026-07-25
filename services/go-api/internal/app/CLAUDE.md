# internal/app — composition root — router

The only place adapters are chosen and wired into ports. Also home to the shared rate-limited HTTP transport, the SSE seam, and the eval/re-run/inspector operator surfaces.

Layout:

- `app.go` — `setup` and the `wire*` stages, shutdown, health, alerts.
- `discovery_wiring.go` / `search_wiring.go` — the discovery context's construction.
- `http_client.go` / `live_transport.go` — provider HTTP client and transport policy.
- `eval_runner.go` — the eval meter's isolated runner.
- `rerun.go` / `rerun_detail.go` / `detail_harness.go` — operator re-run and harness seams.

## Rules

- Build the search pipeline only through `BuildSearchService` — production, eval CLI and eval meter must all go through it.
- Never give a provider its own transport; `defaultLiveTransport` is process-shared so per-host rate limits hold.
- Never use `liveTransport` on the replay path.
- Never give an eval or synthetic search a real event store, and never wire exploration on the `rankingOnly` path.
- Never build the eval runner over production's circuit breakers.
- Never pass a zero-value config to a re-run — it must rank with the live flags.
- Keep `detail_harness.go`'s provider map in sync with `wireDiscoveryContent`'s.
- Pass a nil *interface*, never a typed-nil pointer, where a dependency is optional.
- Never fail startup on a nil dependency — nil Redis/MB/audio-store switch features off. The database is the exception: an empty `DATABASE_URL` must fail setup fast.
- Never 204 an empty SSE replay; emit `resync` on ring gaps.
- Never put query text, user ids or connection strings in an alert message.
- Never let the recording transport affect the search path.
- Drain background goroutines here, not in the scheduler.

Why each rule exists: `okf/backend/app-wiring.md` — read before structural work; update it in the same commit when behavior it describes changes (pre-commit hook enforces).

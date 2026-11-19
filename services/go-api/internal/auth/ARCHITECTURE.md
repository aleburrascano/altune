# Auth — Module Architecture

The auth bounded context (`services/go-api/internal/auth/`) verifies bearer tokens and gates HTTP requests on them. It puts the verified user and token expiry on the request context through `internal/shared/requestuser`, which is where the rest of the backend reads them.

- The door is the package at the module root. `auth.go` re-exports `requestuser.ContextWithUserID` for callers that build an authenticated request, and each `*_door.go` re-exports one part.
- `internal/core/ports/` — what the module needs from outside, `TokenVerifier`, `AuthMetrics` and `TokenRevoker`, and the verifier's contract: `VerifiedToken`, `TokenRejectReason` and `InvalidTokenError`.
- `internal/adapters/middleware/` — the HTTP gate and its per-client failure throttle.
- `internal/adapters/providers/` — verifies Supabase JWTs against the JWKS.
- `internal/adapters/metrics/` — counts outcomes in expvar.
- The tests sit at the module root in `auth_test` and reach the module only through the door; the providers and metrics adapters keep their own adapter tests.

## The failure throttle's prune pass

When the throttle tracks `MaxClients` clients and a new one arrives, `makeRoom` first drops every client whose bucket has fully refilled (`pruneRefilled`), then evicts the fullest buckets down to the low-water mark (`evictFullest`). Below `MaxClients` it evicts nothing.

The prune pass is behaviour. A refilled bucket admits exactly like a client the throttle has never seen, so dropping it loses nothing, and dropping all of them at once can leave the table below the low-water mark, so the next capacity hit comes later. Without the pass, `evictFullest` would stop at the low-water mark with refilled clients still tracked, reach capacity again sooner, and then evict partly spent clients, handing each a fresh burst. `TestFailureThrottle_PruningRefilledClientsSparesAPartlySpentOneFromLaterEviction` pins this through the door.

It also keeps the sort rare. Without the pass, under ordinary churn, where most tracked clients have refilled, every tenth of `MaxClients` new clients triggers a sort of the whole table; with it, the sort that follows covers only the few clients left. Under an attack nobody refills, the pass drops nothing, and the cost is the same either way.

Measured through the door at 10,000 tracked clients over 200,000 admits, alternating with and without the pass for five rounds each:

| Benchmark | With the pass | Without it |
|---|---|---|
| `BenchmarkFailureThrottle_BenignChurn`, worst single admit | 30.9 ms median (27.7–54.4) | 81.2 ms median (55.6–95.7) |
| `BenchmarkFailureThrottle_AttackChurn` | no difference beyond noise | no difference beyond noise |

The host had 3 cores under a load average of about 14. The per-admit medians for benign churn (44 µs with, 70 µs without) overlap because HTTP handling dominates each request, so the worst single admit is the clearer signal. The benchmarks report and do not gate: rerun them with `go test -run '^$' -bench FailureThrottle -benchtime=200000x ./internal/auth/`.

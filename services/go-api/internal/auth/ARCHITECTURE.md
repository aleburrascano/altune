# Auth — Module Architecture

The auth bounded context (`services/go-api/internal/auth/`) verifies bearer tokens and gates HTTP requests on them. It puts the verified user and token expiry on the request context through `internal/shared/requestuser`, which is where the rest of the backend reads them.

- The door is the package at the module root. `auth.go` re-exports `requestuser.ContextWithUserID` for callers that build an authenticated request, and each `*_door.go` re-exports one part.
- `internal/core/ports/` — what the module needs from outside, `TokenVerifier`, `AuthMetrics` and `TokenRevoker`, and the verifier's contract: `VerifiedToken`, `TokenRejectReason` and `InvalidTokenError`.
- `internal/adapters/middleware/` — the HTTP gate and its per-client failure throttle.
- `internal/adapters/providers/` — verifies Supabase JWTs against the JWKS.
- `internal/adapters/metrics/` — counts outcomes in expvar.
- The tests sit at the module root in `auth_test` and reach the module only through the door; the providers and metrics adapters keep their own adapter tests.

## The failure throttle's prune pass

When the throttle tracks `MaxClients` clients and a new one arrives, `makeRoom` first drops every client whose bucket has fully refilled (`pruneRefilled`), then evicts the fullest buckets down to the low-water mark (`evictFullest`).

The prune pass is a cost guard. It changes no outcome: a fully refilled bucket admits exactly like a client the throttle has never seen, and `evictFullest` would evict those buckets first anyway. No test through the door can tell it is there, so removing it leaves every test green.

What it saves is the sort. Without it, under ordinary churn, where most tracked clients have refilled, every tenth of `MaxClients` new clients triggers a sort of the whole map. With it, one linear pass empties the map of refilled clients, the sort that follows covers only the few left, and the next capacity hit waits for a full `MaxClients` of new ones. Under an attack nobody refills, the pass drops nothing, and the cost is the same either way.

Measured at 10,000 tracked clients over 200,000 admits:

| Where | Load | With the prune | Without it |
|---|---|---|---|
| The throttle alone | Benign churn | 4.1–7.3 µs per admit | 54.7–55.8 µs per admit |
| Through the door (`BenchmarkFailureThrottle_BenignChurn`) | Benign churn | worst admit 30.9 ms median (27.7–54.4) | worst admit 81.2 ms median (55.6–95.7) |
| Through the door (`BenchmarkFailureThrottle_AttackChurn`) | Attack churn | no difference beyond noise | no difference beyond noise |

The door-level runs alternated with and without the pass, five rounds each, on a 3-core host under a load average of about 14. Their per-admit medians (44 µs with, 70 µs without) overlap because HTTP handling dominates each request, so the worst single admit is the clearer signal there. The benchmarks report and do not gate: rerun them with `go test -run '^$' -bench FailureThrottle -benchtime=200000x ./internal/auth/`.

# Cost enabler — design

Brief: `docs/cost-enabler.md`.

## Anchor & inherited invariants
- **Anchor:** count go-api's outbound provider calls per provider, exposed operator-only.
- **Inherited:** operator-only · negligible hot-path overhead · no PII.

## Grounded in
- All discovery provider adapters are built with **one shared `transport http.RoundTripper`**:
  `internal/app/search_wiring.go` (`BuildDiscoveryProviders(cfg, transport)`,
  `BuildConsensusProviders`, `BuildSearchServiceWithTransport`), `detail_harness.go`. That shared
  transport is the single seam. Metrics endpoint already exists: `/admin/metrics/live`
  (`internal/admin/handler/metrics_live_handler.go`) + the `expvar` accessors pattern.

## Significance
**Extends existing patterns** (the metrics endpoint + a shared-transport wrap). One wiring point.

## Design decisions
- **Boundaries / the seam:** wrap the shared `http.RoundTripper` in a **counting RoundTripper**
  (new package `internal/discovery/adapters/providermetrics` or `internal/shared/providermetrics`).
  It increments a per-provider, per-outcome counter on each round trip, then delegates. Wired once
  at the transport-build site in `search_wiring.go`.
  - **Provider identity:** map the request **host** → provider (Deezer/Spotify/…); a host not in the
    map counts as `other`. *Over:* thread a provider tag through every adapter — rejected, the host
    map keeps the wrap in one place.
  - **Outcome:** `ok` (2xx), `quota` (429/quota 4xx), `error` (transport fail / 5xx).
- **Exposure:** add the per-provider counts to the operator-only `/admin/metrics/live` response
  (a `providers` field), reusing the existing handler + `OperatorOnly`.
- **Overhead:** atomic counters keyed by a small fixed (provider,outcome) set — no per-request alloc.

## Architectural invariants (spine)
- Counting happens at the one shared transport wrap; adapters are untouched.
- The metrics endpoint stays operator-only; counts carry no URL/query text (host+provider only).

## Slice
**Single leaf:** the counting RoundTripper + its wiring in `search_wiring.go` + the `/admin/metrics/live`
`providers` field + tests (per-provider/outcome counting; operator-only; no-PII). Go-api gate applies.

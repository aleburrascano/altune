# Cost enabler (go-api)

Idea brief. go-api infrastructure that unblocks the provider-usage half of the Cost bucket.

## Vision
Know how much go-api is calling each external provider (Deezer, Spotify, SoundCloud, Apple Music, Amazon Music, YTMusic) — the measurable basis for provider API cost. Today outbound provider calls aren't counted in a readable way.

## The idea
Instrument go-api's **outbound provider HTTP at a shared point** (the provider transport / the existing `httptrace` recorder layer) to count calls **per provider** and **per outcome** (ok / quota-error / failure), and expose them operator-only (extend `/admin/metrics/live` or a sibling endpoint).

**Rejected:** per-adapter manual counters (scattered, miss call sites); an external APM (infra for a solo app).

## Assumptions
- **[load-bearing]** Provider calls funnel through a shared outbound HTTP path that can be wrapped in one place (confirm the exact seam in design).

## Scope / non-goals
**In:** per-provider call counts + outcome, exposed operator-only.
**Out:** converting counts to dollars (the bucket / provider price lists do that); latency (that's the perf metrics).

## Priority
**Must:** per-provider counts exposed. **Then:** per-outcome breakdown.

## Invariants
- Operator-only exposure. Counting adds negligible hot-path overhead. No PII.

## Open questions
The exact shared outbound seam to hook — resolved by grounding in design.

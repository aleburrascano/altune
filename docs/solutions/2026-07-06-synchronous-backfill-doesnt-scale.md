---
date: 2026-07-06
session-context: featured-artists deploy + featured-backfill run
tags: [backend, backfill, scaling, rate-limits, operations]
related-vault: ["wiki/concepts/Backpressure.md"]
---

# Synchronous "backfill the whole library" endpoints don't scale — make them async or incremental

## The pattern

A synchronous HTTP endpoint that iterates a user's **entire** library doing a
rate-limited external call per row (`POST /v1/tracks/featured-backfill` →
`BackfillFeaturedService.Execute`) works fine on a small fixture and then quietly
becomes un-runnable as real libraries grow. It also **re-does all work on every
run** (it re-resolves every track, `ReplaceFeaturedArtists` is idempotent but not
skipped), so a failed/timed-out run makes no net progress toward "done."

## When it bites

- A per-user "backfill / re-enrich everything" use case where each item makes an
  external provider call, especially MusicBrainz (**~1 request/second** hard rate
  limit) or any un-cached lookup.
- The handler comment literally says *"Synchronous — the library is small."* — a
  load-bearing assumption that expires silently. Here: 1,639 tracks took **~27.5
  min** in one request; direct `curl`s timed out at 2 min and 9 min, and each
  restart re-covered the already-done prefix from offset 0.

## What to do

- **Run it detached from the request** as a stop-gap: on the prod VM, launch the
  call under `setsid`/`nohup` with a long `--max-time` so it survives client
  disconnect (Go cancels the handler ctx when the client drops). This is how the
  2026-07-06 run actually completed.
- **Make it async** for real: enqueue a background job (own goroutine +
  `context.WithoutCancel`, or a job row a worker drains), return `202` immediately,
  expose progress via the metrics/rollup surface or a status endpoint.
- **Make it incremental** so re-runs converge: skip rows already resolved
  (`WHERE <derived> IS NULL` / "no featured row yet"), or page with a cursor the
  caller passes back. Idempotent-replace is not the same as skip-if-done.
- **Cache the provider lookups** (the discovery enrichment caches already exist)
  so a second pass is cheap even when it re-scans.
- **Never gate progress on a count that is mostly zeros** — 481/1,639 tracks here
  had `feat.` credits, so the "tracks with featured" count barely moves while the
  scan churns. Log `scanned`/`updated` (this service does) and read *that*.

## Why this is true

External rate limits set a hard floor on wall-clock: N rows × ≥1s/row is a
minutes-to-hours job the moment N leaves the "small fixture" regime. A single
synchronous request can't own that much wall-clock across proxies, client
timeouts, and deploys — and a stateless "re-scan from the top" design turns every
interruption into wasted work instead of resumable progress.

## Anti-pattern to avoid

Shipping a whole-library backfill as a synchronous endpoint "because the library
is small today," with no pagination, no skip-already-done, and no async path. It
passes the fixture test, then can only be run by SSHing to prod and babysitting a
detached `curl`.

## See also

- [vault: wiki/concepts/Backpressure.md], [vault: wiki/concepts/Idempotent Receiver.md]
- `services/go-api/internal/catalog/service/backfill_featured.go` (the endpoint in question)
- `services/go-api/internal/catalog/service/set_track_number.go` (the fill-only, per-item counterpart done right — client-driven, incremental, idempotent)

# Capability: go-api — cost enabler (provider call counting)

Epic #1367 (closed). Leaf: #1371. Design: `docs/cost-enabler.md` (what),
`docs/cost-enabler-design.md` (how). This note is the assembled feature's memory — what now works,
how to read it, and the invariants it holds — confirmed on the whole feature at epic-close, not just
per leaf. It unblocks the **provider-usage half of the Cost bucket** (#1373) by making go-api's
outbound provider call volume readable at all.

## What now works

Every outbound provider HTTP call go-api makes is counted at one shared seam, per provider and per
outcome, and exposed operator-only.

- **One counting wrap.** `providermetrics.CountingTransport`
  (`internal/discovery/adapters/providermetrics/counting_transport.go`) wraps the shared provider
  `http.RoundTripper`. It is wired exactly once, in `newSearchWiring` via
  `countingProviderTransport` (`internal/app/search_wiring.go`), as the outer transport over the live
  transport. Every provider adapter (Deezer, Spotify, SoundCloud, Apple Music, Amazon Music, YouTube
  Music, MusicBrainz, Last.fm) is built on `clientFactory` over that wrapped transport, so the
  adapters themselves are untouched — no per-adapter counters.
- **Classification.** Each round trip is folded to a **provider** by request host suffix
  (`api.deezer.com`/`dzcdn.net` → `deezer`, etc.; any unrecognized host → `other`) and an
  **outcome**: `ok` (2xx/3xx), `quota` (4xx incl. 429), `error` (5xx or transport failure). Counts
  are process-global `expvar.Int` over a fixed `(provider, outcome)` key set built once at package
  init.
- **Exposure.** `GET /admin/metrics/live` gains a `providers` field
  (`internal/admin/handler/metrics_live_handler.go`), a `providermetrics.Snapshot` — a map keyed by
  the fixed provider label, each value `{ok, quota, error}` int64 counts.

Note on semantics: the wrap sits **outside** the live transport's internal retry loop, so it counts
one logical provider call per `http.Client.Do`, not each retry attempt. Redirects the client follows
are each a real outbound hop and each counts.

## How to read it

- `GET /admin/metrics/live` with an operator bearer token. Behind the `/admin` group's
  `authMiddleware` + `OperatorOnly(operatorUserID)` (`internal/app/admin_wiring.go`). Unauthenticated
  → `401`; authenticated non-operator → `403`; empty configured operator id denies everyone.
- Response fragment: `{ ..., "providers": { "deezer": {"ok":N,"quota":N,"error":N}, "spotify":
  {...}, ..., "other": {...} } }`. The key set is always exactly the fixed nine provider labels.

## Invariants it keeps (confirmed at epic-close)

- **Counting at the one shared wrap; adapters untouched.** The only construction site is
  `countingProviderTransport` in `search_wiring.go`; adapters carry no counters.
- **Transparent delegate.** `RoundTrip` returns the base transport's response and error verbatim,
  never mutates the request, and never reads or closes the body — `outcomeFor` reads only
  `resp.StatusCode`. Holds under ok/quota/error/redirect/timeout
  (`TestTransparentDelegate`, `TestCountsPerProviderAndOutcome`).
- **Bounded key set.** `providerForHost` only ever returns a fixed label or `other`, and the counter
  map holds exactly those keys, so a crafted host can never mint a new key or grow memory
  (`TestKeySetIsBounded`, `TestSnapshotHasNoPII`).
- **Concurrency-safe counters.** Counters are `expvar.Int` (atomic) over a map that is immutable
  after init; parallel round trips increment them race-free
  (`TestConcurrentRoundTripsCountExactly`, run under `-race`).
- **No PII.** Only host-derived fixed provider labels and outcome labels are ever recorded — never a
  URL, query, path, header, or body. Snapshot keys are fixed provider labels only. The raw `expvar`
  `/debug/vars` handler is not mounted anywhere (`TestSnapshotHasNoPII`).
- **Operator-only exposure.** The `providers` field is served only by `serveMetricsLive` behind
  `OperatorOnly` (`TestMetricsLive_OperatorOnly`).
- **Negligible hot-path overhead.** One host-suffix fold plus one atomic `Add(1)` per call, after
  the network round trip returns; no lock, no allocation on the counting path.

## Where it lives

- Counting transport + snapshot: `internal/discovery/adapters/providermetrics/counting_transport.go`.
- Single wrap point: `internal/app/search_wiring.go` (`countingProviderTransport`, `newSearchWiring`).
- Endpoint field: `internal/admin/handler/metrics_live_handler.go`
  (route `internal/admin/handler/admin_handler.go`, `/metrics/live`).
- Tests: `internal/discovery/adapters/providermetrics/counting_transport_test.go`.
</content>
</invoke>

// # Two audiences
//
// The server binary (cmd/api) uses App, New and Run alone.
//
// The offline tools under cmd/ use a second, narrower surface: the builders
// that hand them the same object graph the request path gets, so an eval or a
// trace cannot silently score a different pipeline from the one in production.
//
// # Nil-argument contracts
//
// The tool surface is called with nil where the server passes a live
// dependency, so three arguments carry a contract:
//
//   - transport (http.RoundTripper): nil resolves, in newClientFactory
//     (http_client.go), to the one process-wide live transport — per-host rate
//     limiters, capped per-host connections, bounded retries with jittered
//     backoff. A non-nil transport records, replays or traces instead, and
//     every adapter the call builds goes through it.
//   - redisClient: nil is supported throughout. The Redis-backed options are
//     then not wired at all (result cache, artwork cache, identity bridge,
//     MBID index, consensus cache), the cache adapters that are still
//     constructed over it no-op on a nil client, and BuildVocabularyStore
//     returns a nil store, which leaves suggest without a vocabulary. Ranking
//     is unaffected.
//   - pool: nil skips favorites and the identity store (contentSearchOptions
//     guards on it), but the search-history repository is wired over the pool
//     unconditionally and dereferences it on the first write. A nil pool is
//     therefore only safe while nothing records history; every in-repo caller
//     passes a real pool.
//
// # Seam map
//
//   - Lifecycle and HTTP: app.go (App and its dependency fields, New, Run,
//     setup's wiring order, cleanup), routes.go (the router, the middleware
//     every route shares, every mount), latency_middleware.go, and
//     app_shutdown.go, whose ordered table bounds each component's shutdown
//     and carries the one required-predecessor edge (drain before releasing
//     leadership).
//   - Jobs and ticker: jobs.go declares every job's name, kill switch and
//     health snapshot; leader_ticker.go runs them — leader election, per-run
//     leadership scoping, a per-run budget, panic containment;
//     background_jobs.go starts each individual job.
//   - Health and alerting: health.go (GET /health and the individually bounded
//     DB, Redis and auth probes behind it), alerting.go (the alert monitor's
//     conditions and its notifier).
//   - SSE: sse_handler.go (the /v1/events stream, its heartbeat, and the
//     per-user and global connection limits).
package app

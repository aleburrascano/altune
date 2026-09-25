// Package app is the composition root of the go-api service and the seam map
// for it. It holds no domain logic: it decides which concrete adapter each
// module's ports get, mounts the HTTP surface over them, and runs the
// background work beside it. Imports point one way — app imports every module,
// no module imports app — so this is also where app-owned types are mapped
// onto admin/handler's wire DTOs (adminJobs, adminHealthProbe,
// adminEvalRunner in admin_wiring.go) and where one module's types are
// translated into another's ports (catalogOwnedTrackLister and
// catalogTrackNumberSetter in app.go, sharedFeaturedResolver in
// discovery_wiring.go), rather than in the modules themselves.
//
// # Two audiences
//
// The server binary (cmd/api) uses App, New and Run alone.
//
// The offline tools under cmd/ use a second, narrower surface: the builders
// that hand them the same object graph the request path gets, so an eval or a
// trace cannot silently score a different pipeline from the one in production.
// That surface is BuildSearchService, BuildSearchServiceWithTransport,
// BuildRankingOnlySearchService and BuildDiscoveryProviders
// (search_wiring.go), BuildConsensusProviders, BuildArtworkChain and
// BuildVocabularyStore (discovery_wiring.go), BuildArtistContentService
// (artist_content_wiring.go), and NewLiveTransport (live_transport.go). Its
// callers are cmd/discoveryeval (main.go, fixtures.go, detail.go, seed.go),
// cmd/discoverytrace and cmd/backfillartwork. Everything else exported here
// serves the server binary or the admin handler; each addition to the tool
// surface is another way a tool can drift from production wiring.
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
//   - Wiring, one file per module: auth_wiring.go and testauth_wiring.go,
//     catalog_wiring.go, playback_wiring.go, feedback_wiring.go,
//     discovery_wiring.go, search_wiring.go, artist_content_wiring.go,
//     discovery_providers.go, admin_wiring.go. http_client.go and
//     live_transport.go are the single client factory and transport they all
//     build from.
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

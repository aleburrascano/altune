# internal/app — composition root — router

The only place adapters are chosen and wired into ports. Also home to the shared rate-limited HTTP transport, the SSE seam, and the eval/re-run/inspector operator surfaces.

Invariants:

- `BuildSearchService` is the single construction site for the search pipeline — production, eval CLI, and eval meter must all go through it so eval never drifts from what users see.
- Eval/synthetic searches always get a nil event store; exploration is never wired on the `rankingOnly` path.
- `defaultLiveTransport` is process-shared on purpose: per-host rate limits only hold if every provider client shares one limiter. Never give a provider its own transport.
- SSE: never 204 an empty replay (EventSource stops reconnecting); emit `resync` on ring gaps.
- Nil-tolerant degradation is the house style: nil Redis/MB/audio-store switch features off, never fail startup. The database is the exception — `database.NewPool` errors on an empty `DATABASE_URL`, so setup fails fast instead of wiring repos over a nil pool.
- Event publishers get the bus wrapped in admin's `eventtap.Tap` (the Mission Control tap); the SSE handler subscribes to the inner bus directly.
- Shutdown drains the shared background group (corpus refresh, metrics rollup) here, not in the scheduler — without that drain the no-audio-store path leaks goroutines past shutdown. `drainBackground` gives up after its timeout so a hung background task can never wedge shutdown.
- `setup` assembles the graph in dependency order; each `wire*` stage owns one context's construction and the values crossing contexts travel explicitly through the `*Wiring` structs, never through package state.
- Health is tri-state: a nil dependency is `not_configured` (intentionally absent), which is distinct from `down` and must not fail readiness. The per-dependency breakdown stays behind the operator-gated `/admin/health` tile.
- Alert messages built here carry aggregate counts only — never query text, user ids, or connection strings (the coverage condition pages on 24h zero-result counts).

## Eval isolation

The eval meter ships **off** (`EVAL_METER_ENABLED`); when disabled `buildEvalRunner` returns nil and no second provider stack is built at all. When enabled, it gets a dedicated search-service instance with its **own** per-provider circuit breakers, isolated from production's, so an eval failure can never trip a breaker live search depends on. It reuses the pool and Redis, takes a nil event store so synthetic searches don't pollute telemetry, and runs `rankingOnly`: skipping the shared result cache (a cached hit would score the cache, not the pipeline, masking a regression for the whole TTL) and display enrichment (artwork HTTP that title/subtitle matching never reads), while keeping every rank-affecting flag live. `evalSmokeChecks` is deliberately tiny — a handful of canonical queries per interval can't meaningfully burn provider quota, unlike the full-corpus offline harness in `cmd/discoveryeval`.

## HTTP transport policy

The `clientFactory` factories are the single source of provider HTTP-client policy. Every provider construction used to inline its own `&http.Client{Timeout: …}` (~30 sites, a couple silently diverged to 15s), so the policy was invisible and easy to copy wrong. Each factory returns a fresh client with its own connection pool.

`providerRateLimits` caps requests/second per provider host on the **live** path. Exceeding a provider's published limit is what got the IP throttled and surfaced as spurious search timeouts. Unlisted hosts run unbounded (Deezer; SoundCloud carries its own `client_id` throttle). Burst covers one query's per-kind calls (artist + track + album = 3, plus margin) so a single search isn't rate-limited against itself, while the sustained rate still paces throughput *across* queries — which is what providers actually throttle on. A nil limiter for an unlisted host is memoized so it isn't re-checked per request.

`liveTransport` retries 429 and 5xx with a small linear backoff; other 4xx are the caller's fault and are not retried. A cancelled or expired context ends retrying immediately — the caller's budget is gone. A body that cannot be rewound (no `GetBody`) is an error, so the caller stops rather than re-sending a consumed body. It is used for production and **recording only — never replay**, where the `Replayer` answers instantly from fixtures. `roundTripper` exists for providers that own their client and accept only a transport (YouTube Music).

## Operator re-run surfaces

`reRunner` builds providers directly rather than going through `Service`, so it bypasses the live circuit breakers by construction — a re-run can't trip a breaker live users depend on. It must rank with the **live** flag config: passing a zero-value config here once made the waterfall silently diverge from production ranking whenever a flag was on (cross-kind prominence defaults on). `RankExplain` ranks identically to `RankWith` but keeps each result's scoring math. `behavioralScores` reads the live `Service`'s published satisfaction snapshot so a re-run applies the same behavioral input production does.

`detailReRunner` covers the gap `/rerun` leaves — detail is a separate pipeline. It resolves the top artist through the live search `Service`, then fans out the *same* per-seed content calls `useArtistContent` makes and applies the *same* client-side merge (dedupe by title, keep highest track-count, newest-first), against the production service instances, so identity resolution, the MusicBrainz anchor, and the caches all match the phone.

`BuildArtistContentService` in `detail_harness.go` **mirrors `wireDiscoveryContent`'s `artistProviders` map — the two must stay in sync when a content provider is added or removed.** It is a harness seam for the `discoveryeval detail` harness (which passes a seeded in-memory identity store to feed deliberately fractured identities); production still wires through `wireDiscovery`.

## Provider wiring rationale

- **TheAudioDB is deliberately not a search provider**: its free key caps artist search to a single result, which can't serve the ambiguous-query case, and Deezer/MusicBrainz/Last.fm/YouTube already cover artists. It stays an artwork-by-identity resolver in `buildArtworkChain`.
- **Apple Music replaces iTunes for artist content**: same Apple catalog and ids, but the official Catalog API carries release dates, cover art, and ISRC that the plain iTunes lookup misses. iTunes remains a second mainstream source of truth for discography/tracklist, keyed by the `collectionId`/`artistId` its results carry.
- **Apple Music and Spotify serve their own album tracklists natively.** Without them an apple/spotify-sourced album — the norm on identity-bridged cards, where the Deezer group was dropped by the MusicBrainz anchor — has no supported source and falls back to a blind Deezer title search that returns a different album's tracks. The same was true of SoundCloud-sourced singles before SoundCloud resolved its own tracklists.
- **One SoundCloud adapter is shared** across the album, artist and related content maps: each call resolves its own `client_id`, so a shared instance avoids redundant resolution.
- **Last.fm top-tracks is keyed by MBID only** (identity-safe) — the client calls it only for artists with a resolved MBID, so it never falls back to ambiguous name matching.
- **Related tracks are track-keyed** (SoundCloud's numeric track id keys `/tracks/{id}/related`); SoundCloud-only today.
- The featured-artist resolver tolerates a nil MusicBrainz searcher and degrades to Deezer-only — pass a nil *interface*, never a typed-nil pointer, or the degradation check breaks.
- Identity-first V2 detail is enabled by the durable identity store's presence (wired whenever a pool exists); the shared MusicBrainz adapter supplies the authoritative release-group set V2 checks each fan-out provider against, dropping a mis-bridged same-name artist.
- The recording transport wraps the shared live transport so every provider call on a correlated request lands in the drill-down store. It is bounded and degrades silently — it must never affect the search path.

Knowledge base: `okf/backend/app-wiring.md` — read before structural work; update it in the same commit when behavior it describes changes (pre-commit hook enforces).

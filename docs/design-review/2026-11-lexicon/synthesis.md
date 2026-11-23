# Lexicon synthesis: one home per cross-cutting concern

Sources: the 11 inventories in `inventory/` (gitea/main 4cf08f4aa and 7682acc25). Rows marked **[verified]** were re-read in gitea/main 7682acc25 for this synthesis. Everything else traces to the named inventory row. "unverified" carries over from the inventory.
Paths: `ga/` = services/go-api/internal/, `cl/` = client/src/, `ov/` = services/overseer/internal/. Inventory names: err, obs, conc, idio, ids (integration-data-security), prin, arch, beh, cs (creational-structural), fn, test.

## 1. Themes (ordered by payoff against cost)

### T1. One correlation id, from the client to the log line and back
- **Today:** the id is minted at ingress (`ga/shared/httputil/correlation_id.go:15-25`), but the web client sends none (`cl/shared/apiClient/adapters/correlationId.ts:8` returns null on web) **[verified]**. CORS neither allows nor exposes `X-Correlation-ID` (`ga/app/router/router.go:40-53`) **[verified]**, even though prod web is same-origin. The error body has no id (`errors.go:16-19`) **[verified]**, client crash reports drop it (`clientErrorReporting.ts:118-125`), the persisted Job row has no field for it (`ga/acquisition/internal/core/ports/queue.go:17-24`), and 7 scheduler writes plus `runloop.Recover` log with `context.Background()` (`scheduler.go:448-482`, `ticker.go:26`) **[verified]**. The key has 5 spellings (`corr_id`, `correlation_id`, `correlationId`, `corrId`, header). No log line carries a `trace_id`.
- **Fix:** allow and expose the header and delete the web skip. Put the id in every error body, have `ApiError` read it and `reportClientError` attach it. Store it on the Job row, and use `WithoutCancel(jobCtx)` instead of `Background()`. The slog handler adds `trace_id`. Use one key constant.
- **Cost** low (about 10 lines in CORS, plus small edits at each site) · **Payoff** a web failure leads to its server log line (maintainability *Analysability*: "Logs carry the context to trace one request end to end").
- **Merges:** err (correlation, global reporting), obs (request identity, async propagation, vocabulary, trace/log), ids (CORS), idio (detached ctx keeps values), conc.
- **Depends:** none. T3 and T7 build on it.

### T2. One goroutine home: owner, bound, panic guard, detach and wait
- **Today:** `fanout.Group.Go` has no recover (`ga/shared/fanout/fanout.go:15-20`) **[verified]**. 12 hand-written `recover()` blocks drift in whether they log a stack or redact (err), and one is `_ = recover()` (`ga/shared/reqmetrics/middleware.go:23`) **[verified]**. Shutdown waits on one of three Trackers (`ga/app/app_shutdown.go:9-17`) **[verified]**. `Tracker.Launch` has no bound (`tracker.go:22-28`) **[verified]** and labels every panic `search.v2.background_panic`. There are 8 hand-rolled WaitGroup fan-outs, the "detach with budget" idiom is written 9 times (although acquisition already has `detachedTimeout`, `acquire.go:510`) **[verified]**, and "wait with deadline" is written 6 times. Overseer hand-rolls start-once+recover 6 times (`ov/buckets/*`).
- **Fix:** `runloop.Try` / `RecoverContext` is the only panic guard, and `fanout.Group` uses it. Build one Tracker in `ga/app` with a label, budget and max-in-flight (drop and count at the cap), and wait on it before `CloseDatastores`. Promote `detachedTimeout` to `runloop.Detach`, and add `runloop.WaitWithin`. forbidigo bans `recover()`, `sync.WaitGroup` and bare `context.WithoutCancel` outside runloop/fanout.
- **Cost** low to medium (the guard is 3 lines; the migrations are mechanical) · **Payoff** a panic in a provider goroutine no longer kills the process, and background writes finish before the pool closes (reliability *Fault tolerance*, *Availability*: "shut down gracefully").
- **Merges:** err (Try, swallowed failure), conc (fanout safety, trackers, bulkhead, detach, wait), idio (detached ctx), prin (goroutine ownership), cs (overseer lazy init), obs (Recover ctx).
- **Depends:** the forbidigo rules land with go-api's final whole-tree lint (T6).

### T3. One server error boundary
- **Today:** `HandleServiceError` (`ga/shared/httputil/errors.go:38`, 54 callers) is bypassed about 30 times, which produces 5 body shapes (`{detail,code}`, `{detail}`, `{detail,reason}`, text/plain, DTO 404s). It has no `context.Canceled`/`DeadlineExceeded` branch, so a client disconnect becomes a 500 at ERROR **[verified]**. A failure is logged up to 3 times at ERROR: by the handler (`favorites_endpoints.go:35`), by `service.unhandled_error` (`errors.go:44`) and by `request.complete` (`request_logger.go:22-24`) **[verified]**. `CodedError` is copied 3 times (catalog, discovery, observe) plus a 4th variant, and domain errors carry the HTTP status (`ga/catalog/domain/coded_error.go:12`). Only 26% of `%w` wraps name an input.
- **Fix:** one `shared/domain.CodedError` that carries a kind, not a status. One writer emits RFC 9457 (`type,title,status,detail,instance`=corr id, with `code` kept as an extension). It maps Canceled to no body at Info and DeadlineExceeded to 504 `request.timeout`, and logs once (5xx Error, 4xx Debug). Handlers stop logging. Delete `WriteError/WritePlainError/BadRequest/NotFound/InternalError`, ban them with forbidigo, and turn on `wrapcheck`.
- **Cost** medium (about 15 handler files, plus a test that lists every code) · **Payoff** one error per failure, one shape the client can parse, and no false 500s (maintainability *Analysability*; compatibility *Interoperability*).
- **Merges:** err (one boundary, status in domain, cancel, log once, RFC 9457, code catalog, context), obs (duplicate logging), ids (exception shielding), arch (clean-arch exception), idio (package org: net/http in domain).
- **Depends:** T1 (the `instance` field). T8 consumes the code catalog.

### T4. One outbound HTTP stack: client factory, retry, pacing, breaker
- **Today:** the egress retry is linear with ±20% jitter, retries any replayable method including POST, and obeys Retry-After with no cap (`ga/shared/egress/internal/retry/transport.go:112-160`) **[verified]**. ytmusic retries on top of it, so one search can make 6 attempts (`ytmusic.go:27-52`). There are 5 other retry loops with different policies, and only jwks matches reliability.md. HTTP clients are built 3 ways: guarded, InnerTube over `http.DefaultTransport` (`ytminnertube/budget.go:155-157`), and 6 bare `&http.Client{}` (`ytmsearch/client.go:56` has no timeout). musicbrainz, discogs and itunes limit hosts the pacer already limits, and the pacer queues with no depth cap (`pacer.go:57`). Two copies of the same circuit breaker exist (discovery, playback). Request→status→body-cap is hand-rolled in 6 acquisition/observe adapters.
- **Fix:** move `providerhttp` (GetJSON, caps, StatusError renamed `UpstreamStatus`) and egress into one `ga/shared` outbound module with one client factory. Retries are idempotent-only, use `shared/backoff` (exponential, jitter, cap) and cap Retry-After. Delete the ytmusic loop and the adapter limiters, give the pacer a queue cap, and use one `shared/breaker`.
- **Cost** medium (one package move, about 8 adapters) · **Payoff** no blind POST retries and no 6× amplification during a provider outage (reliability *Fault tolerance*: "Retry only idempotent operations… with a limit").
- **Merges:** err (retry, Retry-After), conc (retry, rate limit, breaker, backpressure), beh (command retry, breaker state), cs (factory method, decorator, multiton), idio (outbound JSON), prin (DRY backoff), obs (notifiers bypass egress).
- **Depends:** the acquisition door facades, because the acquisition adapters change.

### T5. One database home: transactions, transient errors, pools, row scans
- **Today:** there are 7 hand-written `pool.Begin`+`defer Rollback` in ga/ and 5 copied `store.begin` in cmd, and the only helper is read-only (`ga/shared/database/readonly.go:11`). Two transient-error classifiers have drifted: catalog counts EOF (`pool.go:53-66`), playback counts DeadlineExceeded and SafeToRetry (`op_faults.go:13-27`). The pool factory is copied into 10 cmd tools. There are 40 hand `rows.Next` loops and 3 homemade collectors, while `pgx.CollectRows` and `BeginTxFunc` are used 0 times. Integration tests have 31 pool helpers and 27 files that read `DATABASE_URL` themselves. Migrations run as the app role (`deploy/prod-migrate.sh:15`), with 0 GRANTs.
- **Fix:** add `database.InTx` (rollback on error and panic) and `database.IsTransient` next to readonly.go. cmd tools call `database.NewPool`. Use pgx's collectors. Add `sharedtest.Pool(t)`/`Tx(t)`. Split the migration role from a DML-only app role.
- **Cost** low (2 packages plus mechanical moves; the role split is a deploy change) · **Payoff** a missed rollback or a misclassified transient error can no longer happen at one site only (maintainability *Modifiability*; security *Confidentiality*, least privilege).
- **Merges:** prin (DRY, transactions), ids (UoW, least privilege, data mapper), idio (cleanup: tx, rows), err (transient classifier), cs (pool factory), test (integration tests).
- **Depends:** the acquisition queue ownership design (which role may `UPDATE tracks`).

### T6. Whole-tree checks that ban the bypass
- **Today:** the checks that would hold T2–T5 in place run on changed lines only, or not at all. depguard, funlen, dupl, unused and testpackage run `--new-from-rev` (`scripts/precheck.sh:146`). The client's `switch-exhaustiveness-check` runs only when `ESLINT_DIFF_SCOPED=1` (`client/eslint.config.js:149-189`) **[verified]**. `exhaustive`, `wrapcheck`, `mnd`, `sqlclosecheck` and a forcetypeassert lint are absent. go-api never runs `-race`, although it has 102 `t.Parallel` and 103 test sleeps (overseer does run it, `precheck.sh:207`). There is no gitleaks or osv-scanner, and govulncheck runs only when go.mod changes.
- **Fix:** a whole-tree, zero-violation config. Enable `exhaustive` (`default-signifies-exhaustive: false`, 18 switches), `wrapcheck` and the forbidigo bans from T2–T4. Run the client typed rules on the whole tree. Add `-race` to `pr-checks.yml:81`, gitleaks and osv-scanner to precheck, and a nightly govulncheck.
- **Cost** low per rule, medium for the backlog it exposes · **Payoff** every "one home" stays one home without anyone remembering it (maintainability *Modularity*: "Boundaries are enforced by the boundary config, not by memory").
- **Merges:** idio (exhaustive switch Go/TS, closecheck), fn (Go ADTs), test (race, static analysis), arch (depguard old code), prin (spaghetti, dead code, magic numbers), ids (secret and dependency scanning).
- **Depends:** go-api's final whole-tree lint and the core-purity lint (both planned), and the fallow boundaries in the client module-spec migration.

### T7. One telemetry pipeline: request outcome, field vocabulary, metrics registry
- **Today:** three middlewares each wrap the writer and measure status and duration, under different keys (`http.route`, `route`, `path`): `otlp/middleware.go:33-57`, `reqmetrics/middleware.go:11-19` and `request_logger.go:9-38` **[verified]**. `request.complete` logs the raw path with no user. `user_id` is attached by hand at 43 sites, so audit is scattered (`track_handler.go:291` logs a delete twice). Each feature keeps package-global expvar counters that `observemetrics` assembles by hand. 407 global `slog.*` calls, and 139 `slog.SetDefault` calls in tests. Every page alert is a cause; there is no symptom alert and no SLOs (14 of 14 TBD).
- **Fix:** one request-outcome middleware feeds the span, the histogram and the log line with route template, corr_id, trace_id and user_id (the auth middleware writes into a holder in the context). Key constants live in `shared/logging`. One metrics registry is built in `ga/app` and passed to each feature's port, and a 5xx-ratio alert reads it. Inject `*slog.Logger` and add `sharedtest.CaptureLogs(t)`.
- **Cost** medium · **Payoff** one measurement per request, an audit line on every write, and alerts on what users feel (maintainability *Analysability*; security *Accountability*).
- **Merges:** obs (outcome, vocabulary, metrics home, user context, page on symptoms, SLOs), ids (audit), prin (globals), test (log spies), cs (singleton: counting_transport).
- **Depends:** T1.

### T8. One client failure model
- **Today:** `ApiError.code` is a bare string, and its message is `API <path> returned <status>` with the server `detail` dropped (`cl/shared/apiClient/adapters/http.ts:161-162`). The user sees it in a banner (`saveTrack.ts:204-206`). HTTP status is classified in 6 places, and `isNetworkError` regex-matches message text (`errors.ts:65-71`). `describeError` is used by 3 screens and turns every 4xx into one sentence. `alertOnError(variables)` never sees the error (`useAppMutation.ts:130`). There are 45 hand-written "Couldn't…" strings, 3 ok-union shapes, and 4 parsers that throw-then-return-null. Retry-After is ignored (`retryDelay.ts:12-15`). On web there is no `unhandledrejection` listener.
- **Fix:** a typed `ApiErrorCode` union generated from T3's catalog, plus `detail`, `retryAfterMs` and the correlation id in `ApiError`. One `Result<T,E>` in shared/failure. `describeError(error, action)` becomes the only copy source (delete `failureCopyForAction`). Add the web listeners.
- **Cost** medium · **Payoff** "Favorites are full (max N)" instead of a path and a 409 (interaction *Self-descriptiveness*: "never a raw status code"; reliability *Faultlessness*).
- **Merges:** err (client Result, string matching, one copy module, plain language, Retry-After), fn (Result/Either client), conc (unhandled rejections), idio (error-probing casts), obs (client logging bypasses).
- **Depends:** T1, T3, and the client module-spec migration (which places shared/failure).

### T9. One home for client async plumbing: mutations, deadlines, cancellation, invalidation
- **Today:** `useOptimisticMutation` is used only by playlists (`cl/shared/playlists/hooks/playlist/mutations.ts:199,243,288`) **[verified]**, and 6 hooks roll back by hand. 11 hand-written `onSuccess` invalidations duplicate server events (a deleted track is invalidated twice). 6 `Promise.race` deadlines bypass `shared/timing/core/deadline.ts`. 6 GETs drop the AbortSignal (lyrics fire once per track skipped). There are 3 debounce copies and 22 raw `staleTime` overrides, 7 of them `Infinity`. The acquisition lifecycle is split across two stores with two backward-guards, plus 13 raw status checks.
- **Fix:** every optimistic write goes through `useOptimisticMutation`. Server events own invalidation through one event→query-key table. Add `withDeadline(work, ms, {onLate})`, `signal?` on every GET adapter, a shared `useDebouncedValue`, named `STALE.*` policies, and one acquisition transition table with predicates.
- **Cost** low to medium · **Payoff** fewer stale fetches and one rollback path (performance *Resource utilization*; interaction *Consistency*).
- **Merges:** beh (memento, client acquisition state), conc (client timeouts, GET signals, debounce), ids (cache invalidation), fn (cache lifetime).
- **Depends:** client module-spec migration (5-part layout).

### T10. Parse once at every boundary
- **Go today:** integer query params follow 3 policies (catalog `limit, _ = strconv.Atoi`, `library_handler.go:79-82`; discovery rejects; observe clamps). There are 3 raw `chi.URLParam` and 1 `DecodeJSON` bypass. 38 `RequireUserID` branches can never fire behind the auth middleware and write a third 401 shape. Event payloads are `map[string]any` at 17 Publish sites (`ga/shared/events/bus.go:101`), and the client already patches `id` vs `track_id` (`acquisitionEvents.ts:46`). TrackId leaks as a string at 6+ signatures.
- **Client today:** decoder helpers are copied in 4 files, with about 16 hand casts and 3 brand bypasses. 7 test files parse Go source with regexes to check the wire contract.
- **Fix:** `httputil.QueryInt` with one reject policy, plus `PathID` and a `withUser` adapter. Typed event structs with a generated TS contract. `contracttest` fixtures that the client decodes with `wireDecoders.ts`, the one decoder home.
- **Cost** medium · **Payoff** malformed input is rejected in one place, and wire drift fails CI (compatibility *Interoperability*; security *Integrity*, economy of mechanism).
- **Merges:** ids (validation, per-handler user checks), idio (decoders, brands, casts, typed IDs), beh (observer bus), arch (domain events), test (consumer contracts), prin (primitive obsession).
- **Depends:** T3 (coded 400s), and the shared/kernel track-length type.

### T11. One clock and one random source
- **Today:** Go has 62 hand-rolled `now func()` fields and 78 direct `time.Now()` calls, 11 of them at decision sites (`ga/catalog/domain/track.go:148,440`, `queue_state.go:249`). Tests have 15 fake clock types, and there is no timer port (25 `NewTimer/After/Sleep`). The client reads `Math.random` in shuffle (`queueOrder.ts:90`), which 34 `jest.spyOn(Math|Date)` calls patch.
- **Fix:** a `shared/clock` port (`Now`, `NewTimer`) with a manual fake in sharedtest, and `now` passed into the 11 decision sites. In the client, shuffle takes `random` and the outbox takes `now`.
- **Cost** medium (constructors change at the root) · **Payoff** the main source of flaky tests and the 103 sleeps go (maintainability *Testability*: "fake timers, never wall-clock time").
- **Merges:** test (clock Go and client, sleeps), fn (pure functions, functional core), prin (injected time), arch (factory: Track clock).
- **Depends:** the core-purity lint (planned), and T6 (`-race`).

### T12. Dependencies visible in constructors, with one composition root per app
- **Go today:** 51 functional options inject collaborators, which produces about 140 `if x.dep == nil` guards. 25 type assertions discover optional capabilities (scoped_store has 7 wrapper variants). Context carries business arguments in acquisition (`acquire.go:153-173`). Required dependencies default to a no-op (`add_track.go:60-64`). 6 types mutate with `With*` after `New`.
- **Client today:** 5 FileStore setters, a `playbackPort()` locator with 10 importers, 9 module stores, `queueResume.ts` writing a global store from domain code, 7 `_reset…ForTest` hooks, and 85 `jest.mock`.
- **Fix:** required collaborators are parameters, optional ones default to Noop, and capabilities live in the port. A `jobRun` struct replaces the context bag. The client builds its ports and stores in one root and passes them down.
- **Cost** high (one service at a time) · **Payoff** what a module needs is visible in its signature, and tests pass fakes instead of patching (maintainability *Testability*, *Modularity*: "a module never imports a ready-made instance").
- **Merges:** idio (options, null object, type assertions, ctx bag, singleton), beh (null object, zustand), fn (module state, null object), cs (builder, singleton, DI), prin (service locator, globals), test (jest.mock, isolation).
- **Depends:** the client module-spec migration (bans jest.mock), and the acquisition/catalog door facades (`New(Deps)`).

### T13. One home for each domain rule
- **Today:** Track's invariants live in 5 places: aggregate methods, service validation, acquisition SQL CASE (`job_queue.go:34-68`), upgrade SQL, and a copy in the fake (`acquisitiontest/ledger.go:106-130`). 33 of 34 fields are exported. The relabel path skips the year check (`track.go:399-401` vs `add_track.go:209`). The playlist cap exists only in SQL, and the memory fake has none. The grade order and "ungraded = D" are restated in SQL. Length tolerance is copied in discovery (`rank/merge.go:356`), durations have 4 shapes, and Streamable is recomputed (`upgrade/runner.go:166`).
- **Fix:** private fields with behaviour methods. One transition function owned by catalog, which both SQL and the fake call or test against. `upgrade.Eligible` as a Go spec. `LengthTolerance` and `Seconds` in the kernel.
- **Cost** high · **Payoff** a rule changes in one file and a fake cannot disagree with Postgres (reliability *Faultlessness*: "Make illegal states unrepresentable").
- **Merges:** arch (entity, aggregate, repository, value object, specification, bounded context), fn (pure transitions), beh (state copies), test (Ledger contract), ids (ACL / table owner).
- **Depends:** the acquisition ledger contract, the bridges moving to internal/app, the kernel track-length type, and the queue ownership design. All are planned.

## 2. Bugs found along the way
- **high** · `fanout.Group.Go` has no recover, so a panic in a provider call kills go-api: `ga/shared/fanout/fanout.go:15-20`, callers `catalog_titles.go:85`, `unreleased/service.go:173` **[verified]**.
- **medium** · Shutdown waits only on the search Tracker. Ownership and discography writes can run after `CloseDatastores`: `ga/app/app_shutdown.go:9-17`, `ownership_enrichment.go:46`, `discography_telemetry.go:22` **[verified]**.
- **medium** · Egress retries any replayable method, POST included, and sleeps any Retry-After uncapped: `ga/shared/egress/internal/retry/transport.go:112-150` **[verified]**. Which outbound POSTs use it is unverified.
- **medium** · ytmusic retries on top of egress, so up to 6 attempts per search: `ytmusic.go:27-52`.
- **medium** · A timeout followed by a second tap creates a duplicate playlist, because there is no idempotency key: `playlist_repo.go:36-45` **[verified]**, `cl/shared/apiClient/playlists.ts:109`.
- **medium** · A client disconnect or request deadline becomes a 500 "internal" plus an ERROR log: `ga/shared/httputil/errors.go:38-50` **[verified]**.
- **medium** · One 5xx produces 3 ERROR lines (handler, `service.unhandled_error`, `request.complete`), and an expected 4xx gets an ERROR line: `favorites_endpoints.go:35`, `errors.go:44`, `request_logger.go:22-24` **[verified]**. 12 handlers do this.
- **medium** · Web requests carry no correlation id although prod is same-origin: `correlationId.ts:8`, `router.go:40-53` **[verified]**.
- **medium** · The user sees "API /v1/… returned 409" in the save banner: `cl/features/detail/core/saveTrack.ts:204-206`, `http.ts:162`.
- **medium** · The relabel path sets Year with no plausibility check: `ga/catalog/domain/track.go:399-401`.
- **medium** · Detached per-search goroutines have no bound and share the 20-connection pool: `ga/shared/runloop/tracker.go:22-28` **[verified]**.
- **medium** · Abandoned FS goroutines on a hung mount grow without bound: `ga/catalog/internal/adapters/storage/filesystem.go:242-270`.
- **low** · The library cap is a count followed by an insert, with no DB guard (race): `ga/catalog/service/track/add_track.go:155-161`.
- **low** · The ytmsearch `http.Client` has no timeout: `ga/acquisition/internal/adapters/ytmsearch/client.go:56`.
- **low** · `reqmetrics` swallows a panic silently: `ga/shared/reqmetrics/middleware.go:23` **[verified]**.
- **low** · Queue settle/release/heartbeat and `Recover` log without the correlation id: `scheduler.go:448-482`, `scheduler_heartbeat.go:31`, `ticker.go:26` **[verified]**.
- **low** · Unhandled rejections and window errors on web are not reported (no listener): `clientErrorReporting.ts:146-155`.
- **low** · Lyrics and 5 other GETs ignore abort, so skipping tracks leaves fetches running: `cl/shared/apiClient/adapters/lyrics.ts:41`, `useLyrics.ts:13`.
- **low** · Parse errors are discarded: `_ = json.Unmarshal` at `ga/acquisition/service/identity.go:141`, `add_track.go:289`.
- **low** · An unknown groupBy is sorted silently, and a test asserts it: `discographyrank/rank.go:20`, `rank_test.go:106`.
- **low** · The playlist cap is only in SQL; the memory fake has none, so tests pass against a lie: `playlist_repo.go:332-343`.
- **low** · A bad `limit` is silently ignored: `ga/catalog/adapters/handler/library_handler.go:79-82`.
- **security** · Migrations run as the app role, so a leaked app credential can run DDL: `deploy/prod-migrate.sh:15`. Web keeps the refresh token in localStorage (`cl/mobile/authStorage.web.ts`, CSP mitigates). `AllowCredentials: true` with no cookies (`router.go`) **[verified]**.
- **unverified** · The shallow Memento copy may alias slices: `acquire.go:422-433`. An upstream `StatusError` matched by duck typing could relay a provider 401/404 as ours: `errors.go:22-25`. journey's downloader drops `WithRemoteComponents`: `journey.go:39-41`.

## 3. Corrections (inventories that disagree)
- **Scheduler queue deadlines.** conc ("no deadline", top-3 #2) is **wrong**, and idio is right. Release, Settle and Heartbeat go through `execFenced`, which applies `WithTimeout(dbCallTimeout)` = 5 s (`job_queue.go:254-255,293-319`, `cooldown_store.go:15`). idio left RefuseQueued (`:482`) unverified; it is covered too, via `settleContext` → `detachedTimeout(ctx, settleBudget)` (`acquire.go:510-515,628-630`) **[verified]**. The real defect is the lost correlation id (T1), and prin's "Ignored cancellation" row resolves the same way.
- **TS exhaustive switch.** prin (Polymorphism: "enforced") and fn (TS ADTs: "set to error") overstate it. idio is right: the rule sits in `house.configs.typed`, which is spread only when `ESLINT_DIFF_SCOPED=1` (`client/eslint.config.js:149-189`, `vendor/eslint-config-house-ts/index.js:103-114`) **[verified]**.
- **Client optimistic rollback.** fn ("done right, one home") is wrong. beh is right: only `shared/playlists/hooks/playlist/mutations.ts` calls `useOptimisticMutation` **[verified]**.
- **Contract suites.** ids (Repository row: "only in catalog") is wrong. `acquisitiontest/job_queue_contract.go` exists **[verified]**. prin's "8" and test's "6" count differently: there are 6 port suites, plus `catalogtest/featured_resolver_contract.go` and the `contracttest` shape helper.
- **Enqueue atomicity.** ids (Outbox row) feared a lost enqueue after the insert. arch is right: the INSERT itself sets `acquisition_available_at` (`track_repo.go:75-83`) **[verified]**.
- **Egress backoff.** err ("linear") and beh ("jitter") are both right: 250 ms × attempt ± 20% (`transport.go:152-160`) **[verified]**. It is still not exponential.
- **Global logger.** obs calls direct `slog.*Context` the home (done right), while prin calls it a global (done wrong). The pipeline is one (`logging.go:9-30`), but access goes through a global. House rule maintainability.md ("never imports a ready-made instance") and test's 139 `SetDefault` swaps side with prin.
- **Mediator (backend).** beh says done right. arch is right that 6 acquisition files import catalog and discovery directly (`catalogbridge/*`, `discoverybridge/*`). The bridges move is planned.
- **Not-found in Go.** err says "scattered", fn says "missing, needed": it is the same finding. "Scattered" fits, since 47 functions already use `(T, bool, error)`.
- **Counts.** conc's own footer says 14 right / 39 rows, but its table has 15 / 40 (script below). The idio/prin interface counts (278 vs 152) and the idio/fn Noop counts (12 vs 23) come from different regexes; not reconciled.

## 4. Rules missing from ~/.claude/quality (checked against all 9 files)
- **maintainability.md, Modularity:** "Each cross-cutting concern (errors, logs, metrics, retries, panics, transactions, validation, time) has one module everything goes through, and a lint rule bans the raw primitive outside it." Nearest: "One concept lives in one place".
- **reliability.md, Fault tolerance:** "Every goroutine has an owner that bounds how many run, recovers its panic through the one shared guard, and is waited on before the resources it uses close."
- **reliability.md, Fault tolerance:** "Work that outlives a request keeps the request's values and gets its own deadline. `context.Background()` is for process roots only."
- **reliability.md, Fault tolerance:** extend the retry line: "…at one layer only (never on top of a retrying client), and a server-sent Retry-After is capped."
- **reliability.md, Faultlessness:** "A caller's cancellation or deadline is not a fault. It maps to its own status and log level, never to a 500 at ERROR."
- **reliability.md, Faultlessness:** "Domain errors carry a kind (not found, conflict, invalid, unavailable). Only the transport boundary maps a kind to a status code."
- **reliability.md, Faultlessness:** sharpen "expected outcomes": "Absence is `(T, bool, error)` or the module's NotFound error, never `(nil, nil)`."
- **reliability.md, Availability:** "Pages fire on what users feel (error rate, latency, SLO burn). Causes are context in the page, not pages."
- **maintainability.md, Analysability:** "Log an error or return it, not both. The boundary logs it once, at a level that matches the outcome: an expected 4xx is not ERROR."
- **maintainability.md, Analysability:** "Every error response and client error report carries the correlation id. Log keys come from one set of named constants."
- **maintainability.md, Analysability:** "Call a collaborator, not its parts (`a.throttle.mu.Lock()`). Traversing plain data is fine."
- **compatibility.md, Interoperability:** "Error responses have one shape (RFC 9457 problem details) and a stable code from one catalog, from which the client's error type is generated."
- **maintainability.md, Modifiability:** "A required collaborator is a constructor parameter and construction fails without it. An optional one defaults to a no-op. Never nil-check a dependency, discover a capability by type assertion, or pass business arguments in context values."
- **maintainability.md, Modularity:** "A rule that decides (a cap, a transition, eligibility) lives in the domain. SQL only narrows and guards (unique index, version), and a test proves the two agree."
- **maintainability.md, Modifiability:** "Code no caller reaches is deleted, and an unused-code check runs on the whole tree."
- **maintainability.md, Testability:** "Concurrent code runs under the race detector in CI." (Sleeps are already banned: "never wall-clock time or sleeps".)
- **maintainability.md, end:** "A check that encodes a house rule runs on the whole tree with zero violations. A changed-lines-only check is a ratchet, and the code it skips is a named backlog."
Already covered, so not proposed: exhaustive unions, contract tests per fake, idempotency keys, cache staleness, bounded queues, secrets scanning, least privilege, boolean flags, named numbers.

## 5. What is genuinely done right
- Ports and adapters with one Go composition root (`ga/app/*_wiring.go`). No DI container, no utils packages, and depguard feature boundaries (prin, arch, idio).
- One HTTP middleware chain for correlation, tracing, metrics, deadlines, logging, recovery, body cap and CORS (`router.go:28-36`). Strict config validation stops the boot (ids, prin).
- Go error idioms: 138 `errors.Is`, 0 type assertions on err, errorlint on, exception shielding at the boundary (err).
- Acquisition worker pool on a leased Postgres queue with fences, heartbeat, and drain-cancel-wait. Typed pipeline stages with rollback and per-step error names (`pipeline.go:51-135`) (conc, beh, arch).
- Security core: one auth middleware, per-user scoping in every repository's SQL, a two-layer SSRF guard (`dial_guard.go:41`), env read only in `shared/config`, and log redaction (ids, obs).
- Metrics hygiene: route-template labels capped at 256, histograms not summaries, and alert dedup with a stable page key (obs). Degraded results are visible to the user (`content_response.go:11-18` → `Section.tsx:73-93`) (err).
- Client type discipline: strict tsconfig, 0 `any`, 0 suppressions (`noInlineConfig`), always-on no-floating-promises, discriminated unions, and one durable telemetry outbox (idio, conc, obs).
- Tests: provider contracts against recorded real payloads, Go mutation testing in CI, 9 of 11 client fakes with contract suites, and fast-check with 43 properties (test).

## 6. Totals (counted by `tools/count_verdicts.py` from each table's Verdict column)
| Inventory | right | scattered | wrong | missing | not needed | n/a | rows |
|---|---|---|---|---|---|---|---|
| architecture | 6 | 8 | 5 | 0 | 5 | 0 | 24 |
| behavioral | 7 | 7 | 4 | 0 | 2 | 0 | 20 |
| concurrency | 15 | 10 | 4 | 3 | 8 | 0 | 40 |
| creational-structural | 5 | 8 | 3 | 1 | 2 | 0 | 19 |
| errors | 11 | 7 | 8 | 4 | 3 | 0 | 33 |
| functional | 8 | 8 | 3 | 2 | 8 | 1 | 30 |
| idioms | 25 | 20 | 4 | 1 | 7 | 0 | 57 |
| integration-data-security | 17 | 6 | 3 | 3 | 10 | 0 | 39 |
| observability | 13 | 8 | 5 | 5 | 2 | 0 | 33 |
| principles | 28 | 8 | 9 | 2 | 10 | 0 | 57 |
| testing | 13 | 7 | 2 | 4 | 2 | 0 | 28 |
| **all** | **148** | **97** | **50** | **25** | **59** | **1** | **380** |

Rows overlap across inventories (retries, recover, correlation id and transactions each appear in 3 to 5), so 380 rows are fewer distinct concepts. Read the totals per topic, not as a score. "done right" counts include rows the inventories marked "unverified" (prin: spaghetti, TDD; test: TDD).

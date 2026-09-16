# Capability: Overseer — platform spine + Live activity

Epic #1154 (closed). Design: `docs/overseer.md` (what), `docs/overseer-design.md` (how).
This note is the assembled feature's memory: what now works, how to run it, and the
invariants it holds — confirmed on the whole feature at epic-close, not just per leaf.

## What now works

A standalone Go service, `services/overseer/` (its own module `altune/overseer`), that
watches the Altune go-api **from outside the process** and presents it as owner-only
web **buckets** (one area of awareness each). It outlives the app it watches: when
go-api is down, Overseer stays up and serves last-known state flagged stale.

- **Plugin core** (`internal/core`): the `Bucket` interface (Collect / Store / Render),
  a `Registry` buckets self-register into, and a bounded `RingStore`. The shell core
  references no concrete bucket — a new bucket is its own files plus one blank import.
- **Shell** (`internal/shell`): owner-only HTTP surface, embedded UI, drives every
  bucket to render a panel. A panicking bucket is contained (`safeRender`); the shell
  stays 200.
- **go-api access** (`internal/goapi`): a read-only REST `Client` (only `Health`), a
  `TokenSource` (operator principal, no write scope), and an SSE `Consumer` that
  reconnects with backoff and exposes a typed source-down `Status`.
- **Live activity bucket** (`internal/buckets/liveactivity`): consumes go-api's operator
  event SSE into a bounded ring, renders a LIVE/STALE feed, degrades to last-known state
  when go-api is unreachable. `heartbeat` and `stub` buckets prove the additive path.
- **Deploy** (`compose.prod.yml` + Caddy + `Dockerfile`): its own container behind Caddy,
  so it survives go-api restarts. **CI** (`.github/workflows/test-overseer.yml`) runs the
  full gate on the required check.

## How to invoke it

- Config (env, `OVERSEER_` prefix): `OVERSEER_OWNER_TOKEN` (required, ≥32 chars — the whole
  security boundary; the service refuses to start without it), `OVERSEER_PORT` (default 8090),
  `OVERSEER_HOST`, `OVERSEER_TICK_INTERVAL` (default 5s), `OVERSEER_ENV`, `OVERSEER_LOG_LEVEL`.
- go-api source (read by the liveactivity bucket): `OVERSEER_GOAPI_URL`, `OVERSEER_GOAPI_TOKEN`.
  Missing/invalid config degrades to a permanently-stale panel (now logged), never a crash.
- HTTP: `GET /health` is open (off-box uptime backstop); `GET /` is the owner-only shell.
  Auth is `Authorization: Bearer <owner-token>` or the `overseer_token` cookie, constant-time.
- Run locally: `cd services/overseer && OVERSEER_OWNER_TOKEN=<32+chars> go run ./cmd/overseer`.
- Gate (pinned toolchain — golangci crashes on Go 1.27): `GOTOOLCHAIN=go1.26.6`, then
  `go build ./... && go vet ./... && golangci-lint run --config ../go-api/.golangci.strict.yml ./...
  && CGO_ENABLED=1 go test -race ./... && govulncheck ./... && nilaway ./...`.

## Invariants it holds (the spine)

Confirmed on the assembled whole at epic-close (green gate + a hostile break pass):

- **observe-only** — the only request builder hardcodes GET; the client exposes no
  mutating method (reflection guard `internal/goapi/observeonly_test.go`).
- **operator, no write scope** — authenticates via a `TokenSource` operator principal only.
- **outlives-the-app** — go-api down → StatusDown → stale panel; shell + `/health` stay up.
- **bounded storage** — RingStore caps retained signals; the SSE events channel is bounded;
  the SSE frame accumulator is now bounded across lines (was the one unbounded path).
- **additive buckets** — core/shell/app depend on no concrete bucket (guard
  `internal/guard/imports_test.go`); registration is one blank import.
- **owner-only** — every data route sits behind the constant-time owner guard; `/health` is
  the only open route and carries no watched-app data.
- **no internal imports** — Overseer imports zero go-api internal packages (module-wide guard).
- **degrade-don't-crash** — a bucket panic is contained on render (`safeRender`) AND on
  collect/store (`safeCollect`/`safeStore`) and in the source goroutine; the process survives.
- **degrade-without-data-loss** — source-down is reported only when nothing fresh arrived;
  the stale flag comes from the consumer Status, not the collect error.
- **render-escaping** — every watched-app field is HTML-escaped before entering `Panel.Body`.
- **owns-its-own-store** — in-memory only; no go-api DB in the dependency graph.

## Hardened at epic-close (whole-feature attack)

The break pass found five defects the per-leaf gates couldn't see; all fixed here:

1. **SSE frame accumulator unbounded (MEDIUM, spine)** — `sse.go` bounded only one line, not
   the multi-line frame; an upstream that never sent a blank separator could OOM Overseer.
   Now capped at `maxEventBytes` across the frame → error → reconnect. Regression test added.
2. **No panic recovery in the collect loop (MEDIUM, spine)** — `collectAll` ran Collect/Store
   in a goroutine with no recover, so a panicking bucket crashed the process (only Render was
   guarded). Added `safeCollect`/`safeStore` + a recover in the bucket's source goroutine.
   Regression tests added.
3. **Vacuous no-internal-imports guard (MEDIUM, false assurance)** — the test scanned only its
   own package (`./...` from the package dir). Now scans `altune/overseer/...` (whole module).
4. **Silent swallow of a malformed `OVERSEER_GOAPI_URL` (LOW)** — a URL typo was
   indistinguishable from go-api being down. Now logged.
5. **Missing framing/sniffing headers on the shell (LOW)** — added `X-Frame-Options`,
   `Content-Security-Policy: frame-ancestors 'none'`, `X-Content-Type-Options: nosniff`.

---

# Capability: Overseer — make it accessible (browser login + refreshing operator token)

Epic #1401 (closed). Children: #1402 (browser login / cookie-set), #1403 (refreshing
operator-token source), #1406 (buckets adopt `SharedTokenSource()`). No separate shape/design
doc — the design lives in the child tickets. Confirmed on the assembled whole at epic-close: full
overseer gate green + a hostile break pass + a live run against production go-api.

## What now works

The Overseer dashboard is openable **in a plain browser** (no devtools), and its buckets stay live
**past the ~1h operator-JWT TTL**.

- **Browser login** (`internal/shell/login.go`, `login.html`): `GET /login` serves a minimal
  owner-token form; `POST /login` validates the token against `OVERSEER_OWNER_TOKEN` in constant
  time (`crypto/subtle`), and on a match sets the `overseer_token` cookie
  (**HttpOnly, Secure, SameSite=Strict, Path=/**) then 303-redirects to `/`. A mismatch re-renders
  the form with a generic, token-free error and sets no cookie (fail closed). The POST body is
  capped at 4 KiB (`maxLoginBody`).
- **Owner-only, two audiences** (`internal/shell/auth.go`): an unauthenticated **browser**
  navigation (GET, `Accept: text/html`, no `Authorization`) is 303-redirected to `/login`; an
  API/curl client still gets a bare **401** with the `Authorization: Bearer` contract unchanged.
  The token is read only from the bearer header or the cookie — **never** a query parameter.
- **Refreshing operator token** (`internal/goapi/refreshing_token.go`): `RefreshingTokenSource`
  exchanges a long-lived Supabase **refresh token** at
  `{OVERSEER_SUPABASE_URL}/auth/v1/token?grant_type=refresh_token` (apikey header, refresh token in
  the JSON body — never the URL), caches the access token, refreshes proactively at ~80% of its
  lifetime (derived from the JWT's own `exp`), coalesces concurrent refreshes into ONE exchange
  (single-flight), and refreshes-on-401. A rotated refresh token is persisted in memory; a blank
  rotated token never overwrites a good one. Every failure is a `*TokenRefreshError` carrying **no
  token material**; `String()`/`LogValue()` redact all secrets.
- **Shared source across buckets** (`internal/goapi/token.go` `SharedTokenSource`,
  `selectTokenSource`): every bucket (reliability, backendperf, domainquality, liveactivity, logs,
  usage, cost) takes its credential from one process-wide `SharedTokenSource()` — so single-flight
  spans the whole fleet. Selection: refresh vars all set → refreshing; else `OVERSEER_GOAPI_TOKEN`
  → static; else a fail-closed null source (buckets degrade to source-down, never crash). Guard
  test `internal/guard/token_source_test.go` fails the build if any bucket constructs
  `StaticTokenSource` directly.

## How to invoke it

- **Open the dashboard:** browse to `https://<overseer-host>/` → you are redirected to `/login` →
  paste the `OVERSEER_OWNER_TOKEN` value → submit → the `overseer_token` cookie is set and you land
  on the dashboard. (Programmatic access is unchanged: `Authorization: Bearer <owner-token>`.)
- **Enable live bucket data (stay live past 1h):** set all three refresh vars —
  `OVERSEER_SUPABASE_URL`, `OVERSEER_SUPABASE_ANON_KEY`, `OVERSEER_GOAPI_REFRESH_TOKEN` — and the
  shared source becomes a `RefreshingTokenSource` (startup logs `mode=refreshing`). With only
  `OVERSEER_GOAPI_TOKEN` set it stays static as before (`mode=static`); with neither, buckets
  degrade to source-down (`mode=null`). `OVERSEER_GOAPI_URL` still names the go-api base.
- Gate (pinned toolchain): `GOTOOLCHAIN=go1.26.6`, then `go build ./... && go vet ./... &&
  golangci-lint run --config ../go-api/.golangci.strict.yml ./... && CGO_ENABLED=1 go test -race
  ./... && govulncheck ./... && nilaway ./...`.

## Invariants it holds (this slice)

Confirmed on the assembled whole at epic-close:

- **owner-only holds end to end** — no prod-reachable bypass of the login; empty configured token
  rejects everything; constant-time compare; the bearer/401 API contract is intact.
- **cookie flags correct** — `overseer_token` is HttpOnly + Secure + SameSite=Strict + Path=/,
  host-only (no Domain).
- **no token in logs/render/URL** — owner, operator access and refresh tokens never reach a log
  sink, the rendered page, or any URL; rejection logs carry only `remote`/`path`; refresh errors
  and `String()`/`LogValue()` are secret-free.
- **refresh is single-flight + concurrency-safe** — proven under `-race`; N buckets due at once
  trigger ONE exchange over the shared rotating refresh token.
- **degrade-don't-crash** — a refresh failure (hostile/absent Supabase, malformed JWT, absurd/
  missing `exp`, non-2xx) returns a typed error; buckets render STALE, the process survives.
- **buckets share one source** — the guard test forbids direct `StaticTokenSource` construction.

## Verified live at epic-close

Ran the built binary against production go-api (`OVERSEER_GOAPI_URL=https://altune.duckdns.org`, a
dummy operator token):

- API GET `/` no token → **401**; browser GET `/` no token → **303 → /login**; `GET /login` →
  200 with framing/sniff headers; wrong token POST → **401, no cookie**; correct token POST →
  **303 → /** with `Set-Cookie: overseer_token=...; Path=/; HttpOnly; Secure; SameSite=Strict`;
  dashboard GET with the cookie (and via `Authorization: Bearer`) → **200**, all buckets render
  **STALE** (the dummy token is rejected by real go-api — degrade-don't-crash), no crash.
- Query-param `?token=`/`?access_token=` never authenticates; an 8 KiB body and a correct token
  padded past 4 KiB are both rejected (MaxBytesReader). With the refresh vars set the binary logs
  `mode=refreshing` and the transport failure surfaces the endpoint URL only — no refresh token or
  anon key. Zero token strings in the process logs.

## Hardened at epic-close (whole-feature attack)

A break pass (3 breakers, ~11 domains) attacked the assembled slice. Two confirmed defects; every
other vector held (no owner-only bypass, constant-time compare with no prefix/timing leak, oversize
body fail-closed, no open redirect, no secret in logs/errors/render/URL, hostile-Supabase parsing/
arithmetic/bounds/timeouts fail closed, the 401 retry can't loop, and all single-flight/concurrency
must-holds held under `-race -count=20`). CSRF on POST /login is correctly unnecessary for a single
shared secret.

1. **Credential-bearing clients followed redirects (HIGH, request-forgery) — FIXED here.** The
   REST client (`client.go`), the refreshing token source (`refreshing_token.go`) and the SSE
   consumer (`consumer.go`) built `&http.Client{}` with no `CheckRedirect`, so a 3xx from a
   compromised/MITM/misconfigured upstream would replay the credential to the redirect target —
   exfiltrating the Supabase **refresh token + anon key** (the refresh POST re-sends its custom
   `apikey` header and body) or the **operator bearer** (SSRF), silently, with `Token()` still
   returning `err==nil`. All three now share `refuseRedirect` (returns `http.ErrUseLastResponse`),
   mirroring the security prober's fenced client — so a credential can only ever reach the
   configured host, on redirects too. Two regression tests added (`refreshing_token_internal_test.go`):
   a token endpoint that 307s to an attacker leaks no apikey/refresh token and caches nothing; a
   go-api that 302s to an attacker is not followed. Both fail without the fix (verified).
2. **Cookie-unsafe owner token silently locks out browser login (MED, fail-closed) — TICKETED
   #1413.** `config.validate` checks token length only; a >=32-char token with a byte
   `http.SetCookie` sanitizes (`;` `\` `"`, a control byte, or >= 0x80) passes startup but yields a
   cookie that diverges from the token → 303 login loop (curl/bearer still works). No bypass. Fix
   is a small decision (reject at startup vs. encode the cookie) → follow-up.

## Not in this slice (roadmap)

Metrics-exposure enabler → Reliability → Back-end performance → Domain quality → Usage →
Front-end health → Security → Cost → remove Mission Control. Persistent (Postgres-owned) store
slots in behind the `Store` interface when history buckets arrive. Visual theme deferred.

---

# Capability: Overseer UI — JSON API + React web app

Epic #1441 (closed). Shape: `docs/features/overseer-ui/shape.md`, design:
`docs/features/overseer-ui/design.md`. Children: #1442 (tracer/skeleton), #1452 (frontend panel
registry), #1456 (auth/SSE hardening), #1443 reliability, #1444 backendperf, #1445 domainquality,
#1446 usage, #1447 cost, #1448 security, #1449 logs panels, #1450 overview + drill-down nav. This is
the "visual theme" slice deferred at the end of the previous capability, now taken — the platform
spine and browser-login capabilities above are unchanged underneath it.

## What now works

Overseer is a control room the owner actually wants to open, not a wall of static HTML. The Go
service is now a **pure JSON API** — every bucket exposes `Snapshot()` (state + typed JSON payload)
instead of hand-rolled HTML — and a real **React + Vite + TS single-page app**, embedded in and
served by the same binary, renders it live.

- **JSON-only backend** (`internal/core`): `Bucket.Render() Panel` is gone; every bucket implements
  `Snapshot() core.Snapshot` (`Meta`, `State` — `live`/`stale`/`source_down` — `UpdatedAt`, a raw
  JSON `Data` payload). `core` no longer imports `html/template`; a guard test enforces it.
- **Supabase login, no cookie** (`internal/shell/auth.go` + `web/src`): the owner signs in with
  their real Supabase account in the browser; the API verifies the JWT and checks a single
  allowlisted user id (`OVERSEER_OWNER_USER_ID`) — any other authenticated Supabase user gets 403.
  Auth is bearer-only in memory; no route sets `Set-Cookie`. The old `/login` HTML form and
  `overseer_token` cookie are retired.
- **Data + live-update surface**: `GET /api/buckets` (owner-guarded) returns every bucket's
  snapshot as JSON; `GET /api/stream` is a bearer-authed SSE fetch-stream (not native
  `EventSource`, which can't carry an auth header) that pushes updates in real time and
  reconnects on a refreshed token past the ~1h Supabase access-token TTL.
- **The SPA** (`services/overseer/web/`, React + Vite + TS, `go:embed`-served under `/overseer/`):
  a design-system shell (Vercel/Linear/Grafana-style, dense-but-legible) with a **frontend panel
  registry keyed by bucket id** mirroring the Go registry — an id with no bespoke panel falls back
  to a generic panel, so a backend-only bucket appears immediately. All 8 buckets now have a
  **bespoke panel**: live activity, reliability, back-end performance, domain quality, usage, cost,
  security, logs.
- **Overview + drill-down navigation** (#1450): a landing page shows every bucket glanceable in one
  view; clicking through (or deep-linking, e.g. `/overseer/reliability`) drills into that bucket's
  full panel, with SPA-side routing (server-side fallback so a direct deep-link still resolves).

## How to reach it

- **Browse to `https://altune.duckdns.org/overseer/`.** The SPA loads (open, static, no auth
  needed to load the shell); it shows a Supabase sign-in; sign in with the owner's real Supabase
  account (not a shared token). On success you land on the overview page with all 8 buckets live.
  Deep-links like `/overseer/reliability` resolve directly via SPA fallback.
- **Programmatic access:** `Authorization: Bearer <supabase-access-token>` against
  `GET /overseer/api/buckets` (all snapshots) or `GET /overseer/api/stream` (SSE); both 401 with no
  token, 403 for a valid-but-non-owner token.
- `GET /overseer/config.json` is open and serves the *public* Supabase URL + anon key so the SPA
  can initialize `supabase-js` for login — it carries no watched-app data.
- `GET /overseer/health` stays open (uptime backstop, unchanged from the spine).
- Local dev: `cd services/overseer/web && npm install && npm run build` produces `dist/`, which the
  Go binary embeds; `cd services/overseer && go run ./cmd/overseer` serves it. CI
  (`.github/workflows/test-overseer.yml`) runs an added frontend step (install/typecheck/lint/test/
  build) alongside the existing Go gate.

## Must-holds it keeps (confirmed live at ship)

- **Single owner only** — a valid non-owner Supabase JWT is rejected 403; only the allowlisted
  user id passes. No RBAC.
- **No cookie** — bearer only; no overseer route emits `Set-Cookie` (checked live on every route:
  `/`, `/config.json`, `/api/buckets`, `/api/stream`, `/health`).
- **Live channel is authed** — `/api/buckets` and `/api/stream` both 401 without a token; verified
  live.
- **Observe-only, preserved** — the JSON API exposes no mutating route; the go-api client stays
  read-only (reflection guard extended to the new surface).
- **Outlives the app** — overseer up ⇒ the SPA loads regardless of go-api; every bucket renders
  `source_down` (last-known state) when go-api is unreachable, never blank, never a crash.
- **Additive on both sides** — a new bucket is its own backend file + 1 registration line AND one
  frontend panel + 1 registry line; neither core references a concrete bucket/panel (guarded on
  both sides).
- **Three states per panel** — every panel renders `live`, `stale`, and `source_down` cleanly.
- **Frontend escaping** — no `dangerouslySetInnerHTML` on watched-app data; React's own escaping
  replaces the retired `html/template` escaping.
- **Carried forward unbroken** — bounded storage, degrade-don't-crash (panic contained on
  collect/store/snapshot), no go-api internal imports.

## Where it runs

Same single-container deploy as before: `altune-overseer` on the OCI prod VM
(`altune.duckdns.org`), behind Caddy at `/overseer/*` (`handle_path` strips the prefix, unchanged).
No new service, no new store — the Go binary now also embeds and serves the built SPA
(`go:embed dist`).

- **Deploy is manual, not CI-wired yet** (runbook: `services/go-api/deploy/RUNBOOK.md`). Unlike
  go-api (auto-deployed by `deploy-backend.yml` on every push to `main`), `services/overseer/**`
  pushes do **not** trigger a deploy — promoting overseer means SSHing to the VM and rebuilding/
  restarting the `overseer` service by hand. #1470 tracks wiring it into CI.
- **Required env** (`services/go-api/.env.production`): `OVERSEER_OWNER_USER_ID` (the allowlist),
  `OVERSEER_SUPABASE_URL`, `OVERSEER_SUPABASE_ANON_KEY` (also served publicly at `/config.json` for
  the SPA), `OVERSEER_GOAPI_URL`, `OVERSEER_GOAPI_REFRESH_TOKEN`. `OVERSEER_OWNER_TOKEN` is retired
  — the binary fails closed without the new vars, never falls back to the old cookie path.
- **Known gotcha, not yet fixed:** the operator refresh token is single-use and rotates; the
  running container holds the rotated token in memory only, so **a restart needs a fresh seed**
  (procedure in the runbook) until #1471 lands (persist the rotated token across restarts).

## Verified live at ship (smoke test, `https://altune.duckdns.org/overseer/`)

- `/overseer/` → 200, real SPA (title "Overseer"); `/overseer/config.json` → 200, public Supabase
  config only; `/overseer/api/buckets` and `/overseer/api/stream` → 401 with no token;
  `/overseer/reliability` (deep link) → 200 via SPA fallback; `/overseer/health` → 200; no
  `Set-Cookie` header on any of the above.
- Owner login verified end to end in a real browser (Supabase account → dashboard).
- Buckets collecting live data: a 25s window logged 0 `collect.failed` and 0 token-refresh errors
  after seeding a fresh operator refresh token per the runbook.

## What broke before / caught in the build

- The prior server-rendered dashboard hand-wrote HTML per bucket, so cohesion was structurally
  impossible and every restyle was 8× work — this is the root problem the split fixes, not a
  regression caught in-flight.
- No interaction bugs were carried to prod: QA's per-slice gates (JSON-only guard, owner-allowlist
  403/401 tests, no-`Set-Cookie` check, SSE reconnect-on-401, frontend registry fallback) held
  through to the whole-feature smoke test above with no new defects found at integration.

## Known follow-ups (open, non-blocking)

- **#1466** — UI polish: OCI JSON field-casing inconsistency, a `dist/` placeholder footgun, some
  unrendered trend fields.
- **#1470** — wire overseer into the CI auto-deploy workflow (currently manual, see runbook).
- **#1471** — persist the operator refresh token across restarts (removes the manual reseed step).

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

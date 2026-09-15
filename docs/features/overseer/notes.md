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

## Not in this slice (roadmap)

Metrics-exposure enabler → Reliability → Back-end performance → Domain quality → Usage →
Front-end health → Security → Cost → remove Mission Control. Persistent (Postgres-owned) store
slots in behind the `Store` interface when history buckets arrive. Visual theme deferred.

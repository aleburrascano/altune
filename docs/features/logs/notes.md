# Capability: Overseer — Logs bucket

Epic #1348 (closed). Design: `docs/logs.md` (what), `docs/logs-design.md` (how).
Platform spine: `notes/overseer.md`. This note is the assembled bucket's memory — what now
works, how to run it, and the invariants it holds — confirmed on the whole bucket at epic-close,
not just per leaf.

## What now works

A live tail of what the watched app is logging, always available inside the owner-only shell.
The Logs bucket (`internal/buckets/logs`) consumes go-api's operator log SSE into a bounded ring
and renders a level-filtered tail, degrading to a last-known STALE view when go-api is down.

- **Second SSE consumer** (`internal/goapi/logs_consumer.go`): `LogsConsumer` streams
  `GET /observe/logs/stream` (moved from `/admin/logs/stream` in #2805) — the events `Consumer`'s
  sibling for the second stream. It **reuses
  sse.go's frame grammar** (the same `splitField` parser, the same `maxEventBytes` cap on both a
  single line and the accumulated multi-line frame, the same `errFrameTooLarge` → reconnect
  signal) and the same reconnect-with-backoff + typed source-down/APIError distinction — but
  decodes `LogRecord`s off its own channel. It **never edits the events `consumer.go`**: the two
  streams stay disjoint (the design decision; factor a shared core only if a third stream appears).
- **Bucket** (`internal/buckets/logs/logs.go`, `render.go`): drains decoded records into a
  `core.RingStore` capped at **200** lines (a bound on top of go-api's already-bounded log ring),
  filters by minimum level, and renders each line — level, message, and every attribute — with
  every field HTML-escaped. It self-registers with one blank import (`cmd/overseer/main.go`) and
  owns all its own files.
- **Degrade path**: when go-api is unreachable and nothing fresh arrived, `Collect` returns
  `errSourceDown` (the shell logs it and skips the store) while `Render` keeps serving the
  last-known tail flagged `STALE`. An unconfigured source (no URL/token, or an invalid URL —
  logged) falls back to a null source that is permanently down, so the bucket renders stale and
  bounded rather than crashing at startup.

## How to invoke it

- Config (env): reuses the platform's go-api source — `OVERSEER_GOAPI_URL`, `OVERSEER_GOAPI_TOKEN`
  (operator bearer). Missing/invalid config degrades to source-down (an invalid URL is logged, so
  a typo is not mistaken for a real outage).
- **`OVERSEER_LOGS_MIN_LEVEL`** — the tail's minimum level (`DEBUG`/`INFO`/`WARN`/`ERROR`).
  Unset or unrecognized shows everything (DEBUG and up), labelled `Level ≥ ALL`; the ranking
  mirrors go-api's own (`internal/observe/handler/streams.go`, moved from
  `internal/admin/handler/logs_handler.go` in #2805) so the two agree on "≥ WARN".
- HTTP: the panel renders inside the owner-only shell (`GET /`). No new route; the consumer's
  target is go-api's `GET /observe/logs/stream`, gated to `OVERSEER_PRINCIPAL_ID`.
- Run locally: configure the go-api source, then run the Overseer as in `notes/overseer.md`
  (`cd services/overseer && OVERSEER_OWNER_TOKEN=<32+chars> go run ./cmd/overseer`).
- Gate (pinned toolchain — golangci crashes on Go 1.27): `GOTOOLCHAIN=go1.26.6`, then
  `go build ./... && go vet ./... && golangci-lint run --config ../go-api/.golangci.strict.yml ./...
  && CGO_ENABLED=1 go test -race ./... && govulncheck ./... && nilaway ./...`.

## Invariants it holds (spine + Logs-specific)

Confirmed on the assembled bucket at epic-close (green gate + a hostile attack pass):

- **render-escaping (every field)** — the message, the level, and every attribute key AND value
  are HTML-escaped before entering `Panel.Body`; the status and level lines are fixed literals
  plus an escaped label. A hostile log line (arbitrary watched-app text — the highest-risk
  surface here) cannot inject markup into the trusted panel. Proven end to end through the
  `toSignal → RingStore → decodeRecord → render` round trip against a payload carrying a hostile
  level, a tag-breakout message, and quotes/angle-brackets in field keys and values
  (`TestRenderEscapesEveryField`, `TestRenderEscapesHostileEverything`).
- **bounded storage** — the tail `RingStore` caps at 200 by construction however long the stream
  runs or however fast records arrive; the consumer's records channel is bounded and applies
  backpressure; the SSE frame accumulator is bounded across lines (`TestRingStaysCapped`,
  `TestLogsConsumerOversizedFrameReconnects`).
- **reuses the SSE decoder, events consumer untouched** — `logs_consumer.go` shares sse.go's
  frame grammar and never edits `consumer.go` (the design decision, held on the assembled whole).
- **degrade-don't-crash** — source down → last-known tail flagged `STALE`; a malformed JSON frame
  is skipped rather than tearing down the stream; an oversized/never-terminating frame surfaces as
  source-down and reconnects; the source pump goroutine contains a panic (`runSource` recover);
  and a bucket panic on collect/store/render is contained by the platform's `safeCollect` /
  `safeStore` / `safeRender` so the shell stays up.
- **level-filter correctness** — filtering mirrors go-api's ranking; unknown levels rank at the
  DEBUG floor so nothing above the floor is silently hidden (`TestLevelFilter`,
  `TestRenderFiltersByConfiguredLevel`).
- **observe-only / owner-only** — the consumer's only request builder is a GET behind the operator
  token; the panel is reachable only through the owner-guarded shell.
- **no go-api internal imports** — the bucket reads go-api only over HTTP via the goapi package.
- **additive** — core/shell/app depend on no concrete bucket; registration is one blank import.
- **clean lifecycle** — reconnect leaks no goroutine/timer per attempt (a per-stream body watcher
  torn down deterministically; the backoff timer stopped), and ctx cancel returns `Run`, closes
  the records channel, and settles goroutines back to baseline
  (`TestLogsConsumerShutdownClosesRecordsNoLeak`, `TestLogsConsumerRunOnce`).

## Attacked at epic-close (whole-bucket attack)

The hostile pass on the assembled bucket found **no defect** — the per-leaf gates already held
the whole. Attacked and clean: XSS via a crafted log message / attribute keys+values / a hostile
level reaching the panel (all escaped, added `TestRenderEscapesHostileEverything` as the
cross-cutting proof); the ring truly bounded under a flood; races between the collect/store path
and the HTTP render (`core.RingStore` is `RWMutex`-guarded; `go test -race` green); goroutine and
timer lifecycle across reconnect + shutdown; malformed and oversized/never-terminating frames;
level-filter correctness incl. unknown levels; and the shell staying up when the bucket panics on
any of collect, store, render, or the source goroutine.

## Not in this slice (roadmap)

Free-text filter over the tail (the "then" in `docs/logs.md`); log search over history /
persistence (bounded ring only — a Postgres-owned store slots in behind the `Store` interface if
history buckets arrive); log-based alerting. This bucket is a prerequisite for eventually retiring
Mission Control's logs tab. Next buckets per `notes/overseer.md`.

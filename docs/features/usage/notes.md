# Capability: Overseer — Usage bucket

Epic #1192 (closed). Design: `docs/usage.md` (what), `docs/usage-design.md` (how).
Built on the Overseer platform spine (`notes/overseer.md`). This note is the bucket's
memory: what it does, how to run it, and the invariants it keeps — confirmed on the
assembled bucket at epic-close (green gate + a live run + a hostile break pass), not
just per leaf.

## What now works

Bucket #7. The Usage bucket (`services/overseer/internal/buckets/usage/`) makes what
the owner actually does in the app — searches, plays, activity over time — visible as
an owner-only panel, built entirely from **bounded rollups aggregated on ingest**. It
never retains raw events.

- **Own SSE consumer.** Usage opens its *own* `goapi.Consumer` (its own connection,
  shared with no other bucket) against go-api's operator event stream. Configured from
  the environment; unconfigured or misconfigured, it degrades to a null source that is
  permanently source-down rather than crashing.
- **Bounded rollups (`rollup.go`).** Every event folds on ingest into three
  fixed-size structures, none of which can grow without limit however long the stream
  runs or however many distinct inputs arrive:
  - **Top-N searches** — a `countMap` capped at 64 distinct queries; at capacity a new
    query evicts the current lowest-count entry (so a flood of unique queries can never
    grow the map, and a one-off never pushes out a frequent query). Render surfaces the
    top 10.
  - **Plays by kind** — a `countMap` capped at 32; in practice ≤7 known playback kinds.
  - **Activity timeline** — one-minute windows folded into a `core.RingStore` capped at
    60 completed windows; only per-window counts are stored, never the raw events.
- **Render (`render.go`).** A LIVE/STALE panel: top searches, plays by kind, activity
  timeline. Every dynamic value (search query, play kind, window label) is HTML-escaped
  before it enters `Panel.Body`.
- **Degrade, don't crash.** When go-api is unreachable the consumer reports
  `StatusDown`; Collect returns source-down only when nothing fresh arrived, so the
  shell keeps the last-known rollups and Render flags them **STALE**. The SSE pump runs
  in its own goroutine with a panic recover, so a misbehaving source cannot take the
  process down.
- **Additive.** The bucket is its own files plus one blank import in
  `cmd/overseer/main.go` (`_ "altune/overseer/internal/buckets/usage"`). It imports no
  go-api internal package and shares no state with any other bucket.

## How to invoke it

- Config (env): shares the platform's `OVERSEER_GOAPI_URL` / `OVERSEER_GOAPI_TOKEN`
  to reach go-api's operator SSE stream; the platform env (`OVERSEER_OWNER_TOKEN`, etc.)
  is in `notes/overseer.md`. Missing/invalid URL or token → permanently-stale panel
  (logged), never a crash.
- View it: `GET /` (owner-only) renders the Usage panel alongside the other buckets.
- Run locally against a stub SSE feed (what epic-close exercised): serve
  `text/event-stream` frames (`data: {"type":"search_performed","timestamp":"…","subject":"jazz"}\n\n`,
  plus `play`/`skip` etc.) at `/observe/events/stream` (moved from `/admin/events/stream` in
  #2805), point `OVERSEER_GOAPI_URL` at it,
  then `cd services/overseer && OVERSEER_OWNER_TOKEN=<32+chars> OVERSEER_GOAPI_URL=<stub>
  OVERSEER_GOAPI_TOKEN=<any> go run ./cmd/overseer`. The panel shows top searches +
  plays by kind + timeline; kill the feed → the panel flips to STALE with last-known
  rollups while `/` and `/health` stay 200.
- Gate (pinned toolchain — golangci crashes on Go 1.27): `GOTOOLCHAIN=go1.26.6`, then
  `go build ./... && go vet ./... && golangci-lint run --config ../go-api/.golangci.strict.yml ./...
  && CGO_ENABLED=1 go test -race ./... && govulncheck ./... && nilaway ./...` from
  `services/overseer/`.

## Invariants it holds (the spine, on this bucket)

Confirmed on the assembled bucket at epic-close:

- **bounded rollups** — top-N searches (64), plays by kind (32), timeline (60 windows)
  are each capped by construction; N×capacity distinct inputs never grow them (proved by
  `rollup_test.go` and a live flood in the break pass). Raw events are never stored.
- **observe-only** — the bucket only reads the SSE stream; it never calls a mutating
  go-api path (the consumer exposes none).
- **owner-only** — the panel is served only behind the shell's constant-time owner guard.
- **degrade-don't-crash** — source-down → STALE last-known panel; the source goroutine,
  Collect, Store, and Render are all panic-contained (`runSource` recover +
  `safeCollect`/`safeStore`/`safeRender`); shell and `/health` stay up.
- **render-escaping** — every watched-app field is HTML-escaped before entering the
  panel (`<script>` query text renders `&lt;script&gt;`).
- **additive bucket** — only its own files plus one registration line; no go-api
  internal imports; its own consumer sharing no state with other buckets.
- **race-free** — the SSE pump ingests while HTTP handlers render, serialized behind the
  aggregator mutex, the RingStore lock, an atomic Status, and a `sync.Once` pump start
  (`go test -race`, plus a concurrency attack in the break pass).

## Hardened at epic-close (whole-feature attack)

The break pass flooded 200k mixed events, raced the pump against Render under `-race`,
fired XSS payloads (RTL-override, NUL, 4 KiB, attribute-context) and hostile timestamps.
Bounded rollups, race-freedom, no goroutine/timer leaks, XSS-escaping, and panic
containment all held. One low-severity robustness gap was found and fixed here:

1. **Future-dated timestamp froze the activity timeline (LOW, observability).** Because
   `timeline.record` folds any older event into the current window, a single event with
   a far-future `Timestamp` (clock skew, an NTP jump, a poisoned event) ratcheted the
   current window into the future; every later real event then folded into that frozen
   window until wall-clock time caught up. Fix: `toSignal` now clamps a future timestamp
   to now (as it already did for a zero timestamp), anchoring the timeline to the
   observer's clock. Regression test `TestFutureTimestampDoesNotFreezeTimeline` added
   (mutation-verified: fails without the clamp). Bounded, no crash — the searches/plays
   rollups were never affected.

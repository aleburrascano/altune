# Capability: Overseer — Reliability bucket

Epic #1191 (closed). Design: `docs/reliability.md` (what), `docs/reliability-design.md` (how).
Platform spine: `notes/overseer.md`. This note is the assembled bucket's memory — what now
works, how to run it, and the invariants it holds — confirmed on the whole bucket at epic-close,
not just per leaf.

## What now works

A second Overseer bucket, `internal/buckets/reliability`, that answers "is the app up, and what
dependency is degraded" from a view that survives go-api going down. It does two structurally
independent things:

- **Mirror** — reads go-api's dependency health (`GET /observe/health`, moved from `/admin/health`
  in #2805, gated to `OVERSEER_PRINCIPAL_ID`) via the read-only
  goapi client's `AdminHealth()` and renders DB/Redis/Auth pills, keeping a bounded ring of
  health samples. When the admin read is unreachable it serves the **last-known** pills flagged
  `STALE` rather than going dark.
- **Own poll** — an **independent reachability poller** on its own goroutine/ticker hits go-api's
  open `GET /health` and derives up/down entirely from its own probes, into its own atomic status
  and own bounded sample ring. This is the **authoritative down-detector**: it shares no state
  with the admin-read path, so "the app is down" can never be conflated with "the admin API is
  degraded". A mirror-only view can't tell you the app is down; this poll can.

Alert mirroring is deliberately out of v1: go-api's alert monitor only writes to its own process
log and holds no readable state (`internal/observe/alert/monitor.go`), so there is nothing to read.
It is a follow-up once go-api grows a readable alerts endpoint.

- **go-api read** (`internal/goapi/health_reads.go`): `AdminHealth()` plus the `OperatorHealth` /
  `HealthDetail` shapes. It is **additive at file level** — its own file, never editing `client.go`
  — and is registered on the observe-only allowlist (`observeonly_test.go`, `readOnlyMethods`), so
  the observe-only invariant cannot regress silently.
- The bucket owns all its files and self-registers with one blank import (the additive-buckets
  invariant); the shell core references no concrete bucket.

## How to invoke it

- Config (env): reuses the platform's go-api source — `OVERSEER_GOAPI_URL`, `OVERSEER_GOAPI_TOKEN`
  (operator bearer). Missing/invalid config degrades to a null client: the poll reports **down** and
  the mirror renders **stale** rather than crashing the service (an invalid URL is logged, so a typo
  is not mistaken for a real outage).
- **`OVERSEER_RELIABILITY_POLL_INTERVAL`** — the own-poll cadence (a Go duration, e.g. `15s`).
  Defaults to `30s`; a non-positive or unparseable value is refused in favour of the default (with a
  log line), never silently disabling the detector.
- HTTP: the panel renders inside the owner-only shell (`GET /`). No new route; the poll's own probe
  target is go-api's open `/health`.
- Run locally: configure the go-api source, then run the Overseer as in `notes/overseer.md`
  (`cd services/overseer && OVERSEER_OWNER_TOKEN=<32+chars> go run ./cmd/overseer`).
- Gate (pinned toolchain — golangci crashes on Go 1.27): `GOTOOLCHAIN=go1.26.6`, then
  `go build ./... && go vet ./... && golangci-lint run --config ../go-api/.golangci.strict.yml ./...
  && CGO_ENABLED=1 go test -race ./... && govulncheck ./... && nilaway ./...`.

## Invariants it holds (spine + Reliability-specific)

Confirmed on the assembled bucket at epic-close (green gate + a hostile attack pass):

- **independent down-detector** — the poll path shares no field with the admin-read path (distinct
  interfaces `reachChecker` / `healthReader`, distinct state); it reports **down even when go-api is
  fully down**, and a degraded admin API is never reported as an outage. Proven by
  `TestPollSignalIndependentOfAdminRead` across every admin×poll cross.
- **observe-only** — the only new go-api method is a GET; it is on the client's read-only allowlist.
- **bounded storage** — mirror history and poll samples are separate `RingStore`s, each capped at
  120 by construction no matter how long the service runs (`TestPollerBoundedSamples`).
- **degrade-don't-crash** — admin read unreachable → last-known pills flagged `STALE` while the poll
  stays live; an unconfigured source degrades to poll-down + stale-mirror, never a startup crash;
  and the poll's background goroutine now contains a panicking probe (see below).
- **owner-only** — the panel is reachable only through the owner-guarded shell.
- **render-escaping** — every watched-app field (dependency statuses, error text, history text) is
  HTML-escaped before entering `Panel.Body`; the reachability line is a fixed literal. A hostile
  go-api response cannot inject markup into the trusted panel.
- **additive** — `health_reads.go` never edits `client.go`; core/shell/app depend on no concrete
  bucket; registration is one blank import.
- **no go-api internal imports** — the bucket reads go-api only over HTTP via the goapi client.

## Hardened at epic-close (whole-bucket attack)

The attack on the assembled bucket found one defect the per-leaf gates couldn't see; fixed here:

1. **Unguarded reachability-poll goroutine (MEDIUM, degrade-don't-crash spine)** — `poller.run`
   spawns a long-lived background goroutine (`go b.poller.run(ctx)`) that lives **outside**
   `safeCollect`'s recover, yet had no panic recovery of its own — unlike the liveactivity and usage
   source pumps, which the platform epic-close explicitly guarded for exactly this reason. A panic in
   `pollOnce` or the checker would crash the whole Overseer process. Fixed by wrapping each probe in
   `safePollOnce` (recover + log), so a single misbehaving probe crashes neither the process nor the
   detector — the ticker survives and the next probe runs. Regression test
   `TestPollerContainsProbePanic` (verified to crash the process without the guard).

Attacked and clean: poll/admin independence under failure, goroutine/timer lifecycle across the
poller + ctx-cancel shutdown (bound to the app-lifetime ctx, `TestPollerRunExitsOnCancel`), races
between poller/mirror/render (`go test -race` green), XSS via crafted dependency-error text, shell
staying up when a bucket panics, and both rings truly bounded.

## Not in this slice (roadmap)

Alerts mirror (blocked on go-api growing a readable alerts endpoint), alert history, and
SLA/uptime-% math. Next buckets per `notes/overseer.md`: Back-end performance → Domain quality →
Front-end health → Security → Cost.

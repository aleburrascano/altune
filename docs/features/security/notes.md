# Capability: Overseer — Security bucket (fenced self-test prober)

Epic #1369 (closed). Design: `docs/security.md` (what), `docs/security-design.md` (how).
Platform spine: `notes/overseer.md`. This note is the assembled bucket's memory — what now
works, how to run it, and the invariants it holds — confirmed on the whole bucket at epic-close,
not just per leaf.

## What now works

Continuous, live proof that go-api still rejects the attacks we've been hardening against. The
Security bucket (`internal/buckets/security`) is the Overseer's first **active** bucket: on its
own low-frequency ticker it fires a fixed suite of **safe** self-tests at go-api's own surface and
renders a pass/fail panel, degrading to a last-known STALE verdict when go-api is unreachable.

- **Fenced prober** (`prober.go`): the bucket's OWN raw `http.Client` — it must send
  unauthenticated and deliberately malformed requests, so it never reuses the operator
  `goapi.Client` and never carries the operator token. Its crux is the **fence**: `validateTarget`
  checks every probe's host against an own-infra allowlist BEFORE a socket is opened; an
  off-allowlist target returns `errOffAllowlist` and the `http.Client` is never touched. The base
  URL is fixed at construction and every probe is a path joined onto it (`base.JoinPath`), so a
  path can never move a request to another host — the fence is the structural belt on top. The
  client **fails closed** at construction (a base host absent from its own allowlist is a build
  error, surfaced as a source-down null prober, not a permanently-refused runtime probe) and
  **refuses to follow redirects** (`CheckRedirect` → `http.ErrUseLastResponse`), so a 3xx cannot
  bounce a probe to a host the fence never cleared. The drain-for-reuse is bounded (`maxProbeBody`,
  1 MiB) and every probe is time-bounded (`probeTimeout`, 10 s).
- **Safe suite** (`suite.go`): four read/rejection assertions grounded in go-api's surface
  (`internal/app/routes.go`) — unauthenticated `/v1` → 401/403, unauthenticated `/observe/health`
  (the `observe-gate` check, moved from the `/admin` operator-gate probe in #2805) → 401, a
  burst → shed with 429, a malformed read → a clean 4xx, never a 500. A check PASSES only when the
  app answers with a rejection status; a 2xx (defense let it through) or a 5xx (app fell over)
  fails it. Every probe is a GET with no body — **no probe can mutate go-api state by
  construction**.
- **Scheduler** (`scheduler.go`): a ticker (default hourly) that fires once immediately then on
  every tick until ctx is done. It is **recover-guarded** (`safeRunOnce` contains any probe panic
  — the run loop lives OUTSIDE the shell's `safeCollect` recover, so containment lives here) and
  **ctx-bound** (returns on cancellation, leaking no goroutine past the app's lifetime). A
  non-positive interval is clamped to the default so the ticker can never be disabled.
- **Bucket + render** (`security.go`, `render.go`): results flow through the scheduler's sink into
  a bounded `core.RingStore` (capped at **120** summaries). A run where NO check reached go-api is
  a source outage → the last-known verdict is preserved and flagged STALE (degrade, don't go dark);
  a reachable run replaces it and clears stale. Every dynamic part of the panel — statuses and any
  reflected error text (a fence refusal or transport error can carry a probe target) — is
  HTML-escaped. Self-registers with one blank import (`cmd/overseer/main.go`) and owns all its own
  files (the additive-buckets invariant).

## How to invoke it

- Config (env): reuses the platform's go-api source — **`OVERSEER_GOAPI_URL`** is the probe base.
  When unset the bucket builds a null prober and renders stale rather than crashing at startup.
  Note it does NOT use `OVERSEER_GOAPI_TOKEN`: the prober is deliberately unauthenticated.
- **`OVERSEER_SECURITY_ALLOWLIST`** — comma-separated own-infra hosts the fence permits. Unset
  defaults to the configured go-api host and nothing else (the fence is the explicit, testable
  gate, not implicit trust of the base URL). An explicit list that OMITS the base host **fails
  closed** in `newFencedClient` — the misconfiguration surfaces as a warning + a source-down null
  prober, never a permanently-refused live probe. Host-only: entries are normalized to a bare
  lowercase hostname (scheme/port/case stripped), so the fence compares like with like.
- **`OVERSEER_SECURITY_INTERVAL`** — the self-test cadence (a Go duration, e.g. `30m`, `2h`).
  Default `1h`. A non-positive or unparseable value is refused in favour of the default (logged)
  rather than silently disabling the self-test.
- HTTP: the panel renders inside the owner-only shell (`GET /`). No new route; the prober's target
  is go-api's own surface (`/v1/*`, `/observe/*`, discovery), never anything off the allowlist.
- Run locally: point at a go-api, then run the Overseer as in `notes/overseer.md`
  (`cd services/overseer && OVERSEER_OWNER_TOKEN=<32+chars> OVERSEER_GOAPI_URL=<url> go run ./cmd/overseer`).
- Gate (pinned toolchain — golangci crashes on Go 1.27): `GOTOOLCHAIN=go1.26.6`, then
  `go build ./... && go vet ./... && golangci-lint run --config ../go-api/.golangci.strict.yml ./...
  && CGO_ENABLED=1 go test -race ./... && govulncheck ./... && nilaway ./...`.

## Invariants it holds (spine + Security-specific)

Confirmed on the assembled bucket at epic-close (green gate + a hostile attack pass):

- **Probes-stay-home (the crux)** — every probe's host is validated against the own-infra allowlist
  BEFORE a socket opens; off-allowlist → `errOffAllowlist`, no request sent. Proven structurally
  (a counting/recording transport shows zero requests leave) against userinfo smuggling
  (`allowed.test@evil.example`), suffix/prefix/substring adjacency, a trailing-dot host, loopback /
  cloud-metadata / IPv6 IPs, and an empty host — all refused; case-folding of the SAME owned host
  is correctly allowed; a hostile path/query (`//evil`, `@evil`, a fragment authority) cannot move
  the request off the fixed base host (`TestFenceRefusesOffAllowlistNoRequestSent`,
  `TestFenceHostConfusionRefused`, `TestHostilePathCannotMoveHost`, `TestFenceCaseFoldedHostAllowed`).
- **fail-closed construction** — a base host absent from its own allowlist, or an explicit
  allowlist omitting the base, is a build error → source-down null prober, never a live probe
  (`TestNewFencedClientFailsClosed`, `TestExplicitOffAllowlistConfigFailsClosed`).
- **no state-mutating probe** — every request is a GET with `http.NoBody`; the suite only reads and
  asserts rejections, so no probe can mutate go-api state by construction (`TestProbeIssuesGetOnly`).
- **no redirect bypass** — the client refuses to follow 3xx, returning it as-is rather than chasing
  it to an uncleared host (`TestFenceBlocksRedirectOffAllowlist`).
- **scheduler recover-guarded + ctx-bound** — a panicking probe is contained and the ticker
  survives (the run loop is outside the shell's recover, so `safeRunOnce` owns containment); ctx
  cancel returns `run` with no goroutine leak; a non-positive interval is clamped
  (`TestSchedulerContainsProbePanic`, `TestSchedulerExitsOnCancel`, `TestSchedulerClampsInterval`).
- **degrade-don't-crash** — a fully-down run preserves the last-known verdict flagged STALE and
  clears it on recovery; an unconfigured bucket renders stale/empty, never a panic
  (`TestDegradeToStale`, `TestStaleClearsOnRecovery`, `TestUnconfiguredDegrades`).
- **bounded storage** — the history `RingStore` caps at 120 by construction however long the
  service runs; the probe body drain is capped at 1 MiB (`TestBoundedHistory`).
- **render-escaping** — every reflected probe text (a fence refusal or transport error carrying a
  target) is HTML-escaped before entering `Panel.Body`; a poisoned error string comes out inert
  (`TestRenderEscapesReflectedText`).
- **observe-only / owner-only / additive / no internal imports** — the prober is unauthenticated
  and GET-only; the panel is reachable only through the owner-guarded shell; the bucket reads
  go-api only over HTTP and self-registers with one blank import.

## Attacked at epic-close (whole-bucket attack)

The hostile pass on the assembled bucket, focused on the FENCE (this is the fleet's only outbound
bucket), found **no defect** — the per-leaf gates already held the whole. Attacked and clean:
every path to sending a request goes through `validateTarget` (host / suffix / prefix / substring /
userinfo / case / trailing-dot / IP-vs-host confusion — all refused with zero requests sent, added
`TestFenceHostConfusionRefused` as the cross-cutting proof); a hostile path or query cannot smuggle
an authority past the fixed base host (`TestHostilePathCannotMoveHost`); redirect bounce refused;
an off-allowlist config fails closed (`TestExplicitOffAllowlistConfigFailsClosed`); the GET-only
no-mutation property is structural; the scheduler goroutine drains on shutdown with no leak; a
probe panic is contained so the shell stays up; reflected text is escaped; history stays bounded.

**Known residual (out of scope, confirmed not a defect):** DNS-rebinding of an *already-owned* host
name — the fence is a **host** allowlist, so once `allowed.test` validates, the OS resolver connects
to whatever IP DNS returns at that instant. Defending against a rebound IP needs IP-pinning /
resolve-then-dial control, which `docs/security.md` explicitly scopes out (own-infra allowlist, not
an IP fence). The realistic threat here is off-allowlist targets, which the fence refuses outright.

## Not in this slice (roadmap)

More checks and scheduling controls (the "then" in `docs/security.md`); real exploitation /
fuzzing, external scanners, and destructive/DoS testing are explicitly rejected. Persistence slots
in behind the `Store` interface if history buckets arrive. Next buckets per `notes/overseer.md`
(Cost, then removing Mission Control).

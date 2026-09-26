# Capability: Overseer — Domain quality bucket

Epic #1347 (closed). Design: `docs/domain-quality.md` (what), `docs/domain-quality-design.md` (how).
Built on the Overseer platform spine (`notes/overseer.md`). This note is the bucket's
memory: what it does, how to run it, and the invariants it keeps — confirmed on the
assembled bucket at epic-close (green gate + a hostile break pass), not just per leaf.

## What now works

Bucket #6. The Domain quality bucket (`services/overseer/internal/buckets/domainquality/`)
answers, at a glance, is the core product actually good right now — is search returning
quality results, and is acquisition succeeding? It mirrors two operator reads on go-api's
public surface through the read-only goapi client; it never re-runs any pipeline.

- **Two independent source reads (`internal/goapi/eval_reads.go`).** Additive file,
  never edits `client.go`:
  - `AdminEval` → `GET /observe/eval` (moved from `/admin/eval` in #2805) → `EvalStatus`: the
    in-process eval-meter score vs a baseline. `Score`/`Baseline`/`LastRun` are pointers (nil =
    "not scored yet", distinct from a real zero). Unknown fields a newer go-api adds are ignored
    (version-skew tolerant).
  - `AdminAcquisition` → `GET /observe/acquisition` (moved from `/admin/acquisition` in #2805) →
    `AcquisitionStatus`: the aggregate success rate `succeeded/(succeeded+failed)` plus
    in-flight/queue/rejected gauges.
  - Both go through the client's guarded `get` primitive, so they inherit the bearer gated to
    `OVERSEER_PRINCIPAL_ID`, the host pin (the token can only ever reach the configured go-api host), the
    bounded response body, and the end-to-end timeout. Both are registered in the
    observe-only allowlist (`observeonly_test.go`); they are pure reads.
- **Independent degrade (`domainquality.go` Collect).** Each side records fresh on
  success or is flagged **STALE** on failure while its last-known value is preserved. The
  two are independent: an eval read failing never disturbs a working acquisition read, and
  vice versa. Collect returns an error (`errBothDown`) **only when BOTH reads are
  unreachable** — so the shell logs a genuine outage but a single failure never suppresses
  the half-live panel.
- **Bounded history.** Samples fold into a `core.RingStore` capped at 120 entries; memory
  is bounded by construction however long the service runs.
- **Render (`render.go`).** An eval block (score vs baseline, state, per-query verdicts)
  and an acquisition block (success rate, gauges), each flagged STALE when its read is
  currently unreachable while still showing its last-known value — degrade, don't go dark.
  Every dynamic field — all watched-app data (eval state/error, query strings, rate/gauge
  text, history samples) — is HTML-escaped before entering `Panel.Body`.
- **Undefined success rate is not zero.** `AcquisitionStatus.SuccessRate()` returns
  `(0, false)` when there are no completed jobs, so the panel renders "n/a — no completed
  jobs" rather than a misleading 0% or a divide-by-zero. Rejected admissions are excluded
  (they never ran).
- **Unconfigured degrades, never crashes.** Missing/invalid `OVERSEER_GOAPI_URL` /
  `OVERSEER_GOAPI_TOKEN` yields a null reader: both sides report source-down and render
  STALE (invalid URL is logged), rather than failing the service at startup.
- **Additive.** The bucket is its own files plus one blank import in `cmd/overseer/main.go`
  (`_ "altune/overseer/internal/buckets/domainquality"`). It imports no go-api internal
  package and shares no state with any other bucket.

## How to invoke it

- Config (env): shares the platform's `OVERSEER_GOAPI_URL` / `OVERSEER_GOAPI_TOKEN` to
  reach go-api's `/observe/eval` and `/observe/acquisition` (moved from `/admin/eval` and
  `/admin/acquisition` in #2805); the platform env
  (`OVERSEER_OWNER_TOKEN`, etc.) is in `notes/overseer.md`. Missing/invalid config →
  permanently-STALE panel (logged), never a crash.
- View it: `GET /` (owner-only) renders the Domain quality panel alongside the other buckets.
- Run locally against a stub go-api (what epic-close exercised): serve JSON at
  `/observe/eval` (e.g. `{"state":"ok","score":0.92,"baseline":0.85,"queries":[{"query":"jazz","passed":true}]}`)
  and `/observe/acquisition` (e.g. `{"succeeded":40,"failed":2,"in_flight":1,"queue_depth":3,"queue_capacity":16,"rejected":0}`),
  point `OVERSEER_GOAPI_URL` at it, then
  `cd services/overseer && OVERSEER_OWNER_TOKEN=<32+chars> OVERSEER_GOAPI_URL=<stub>
  OVERSEER_GOAPI_TOKEN=<any> go run ./cmd/overseer`. The panel shows the eval score vs
  baseline + acquisition success rate; take down one endpoint → only that half flips to
  STALE with its last-known value while the other stays live and `/` + `/health` stay 200.
- Gate (pinned toolchain — golangci crashes on Go 1.27): `GOTOOLCHAIN=go1.26.6`, then
  `go build ./... && go vet ./... && golangci-lint run --config ../go-api/.golangci.strict.yml ./...
  && CGO_ENABLED=1 go test -race ./... && govulncheck ./... && nilaway ./...` from
  `services/overseer/`.

## Invariants it holds (the spine, on this bucket)

Confirmed on the assembled bucket at epic-close:

- **independent degrade** — eval and acquisition degrade separately; one source down flags
  only its half STALE and preserves its last-known value, the live half is untouched;
  Collect errors only when BOTH are down (`errBothDown`).
- **bounded storage** — the history RingStore is capped at 120 by construction; no path
  can grow it without limit.
- **observe-only** — both reads are GETs in the allowlist (`observeonly_test.go` reflection
  guard); the client exposes no mutating method.
- **owner-only** — the panel is served only behind the shell's constant-time owner guard.
- **degrade-don't-crash** — source-down → STALE last-known panel; the null reader covers
  unconfigured; Collect, Store, and Render are panic-contained by the shell
  (`safeCollect`/`safeStore`/`safeRender`); shell and `/health` stay up.
- **render-escaping** — every watched-app field is HTML-escaped before entering the panel
  (`<script>` in a query string renders `&lt;script&gt;`).
- **additive bucket / additive reads** — only its own files plus one registration line;
  `eval_reads.go` is a new file that never edits `client.go`; no go-api internal imports.
- **race-free** — the Collect loop writes last-known snapshots + stale flags under `mu`
  while HTTP handlers Render under the same lock; snapshots are copied to the heap and
  replaced, never mutated in place; the RingStore is internally locked (`go test -race`
  plus a concurrency attack in the break pass).
- **no divide-by-zero / no overflow** — `SuccessRate()` returns undefined `(0, false)` on
  zero completions and takes the completed-jobs sum in float64, so a hostile go-api can
  never wrap the count (which would spuriously report "no data" or a rate past 100%).

## Hardened at epic-close (whole-feature attack)

The break pass attacked all six named surfaces across three breakers (~34 techniques):
the independent-degrade invariant under every up/down/recovery combination, Collect-vs-Render
races and the RingStore under an 8×8 concurrent probe (`-race` clean), XSS via crafted eval
query/state/error and acquisition fields (all escaped; `template.HTML` cast sound),
bounded history (200k Adds → Len==Cap==120), shell survival under bucket panic
(`safeCollect`/`safeStore`/`safeRender` contain it — the bucket spawns no escaping
goroutine), and the `SuccessRate`/`Scored` arithmetic edges. Two real LOW-severity defects
were found:

1. **uint64 overflow in `SuccessRate` (LOW, arithmetic) — FIXED here.** `succeeded+failed`
   was a raw uint64 add: a hostile/corrupt go-api response with counters near the ceiling
   wrapped the sum — `2^63+2^63` wrapped to 0 (guard returned false → spurious "n/a — no
   completed jobs" for 2^64 jobs), and `(2^64-1)+5` wrapped to 4 (rate rendered as
   ~4.6e11%). Fix: take the sum in float64, which spans the whole uint64 range without
   wrapping and is exact below 2^53 (so no change for any benign counter). Regression test
   `TestAdminAcquisitionSuccessRateSurvivesCounterOverflow` added (exact-wrap, partial-wrap,
   both-at-ceiling). Only reachable under a hostile go-api — precisely the adversary the
   render layer hardens against — so worth closing.
2. **A source that never once succeeds fails invisibly (LOW–MEDIUM, visibility) — TICKETED
   #1377.** If one endpoint 404s on every collect from startup while the other succeeds, the
   failing side renders "no … mirrored yet" (no STALE banner, nil last-known) and Collect
   logs nothing (only both-down errors). Not fixed here: closing it changes the render/log
   contract and rewrites the shipped `TestIndependentDegrade`, a decision rather than a
   mechanical fix. Narrow window (persistent per-endpoint break from process start); once a
   side succeeds once, STALE works correctly.

Everything else held: no injection, no race, no unbounded growth, no crash-the-process path,
and startup degrades to a null reader on all 13 hostile config values.

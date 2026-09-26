# Capability: Overseer — Cost bucket (provider usage + OCI infra spend)

Epic #1368 (closed). Design: `docs/cost.md` (what), `docs/cost-design.md` (how).
Platform spine: `notes/overseer.md`. This note is the assembled bucket's memory — what now
works, how to run it, and the invariants it holds — confirmed on the whole bucket at
epic-close, not just per leaf. **This is the first bucket that reads a source other than
go-api** (OCI's usage-api).

## What now works

The Overseer's Cost bucket, `internal/buckets/cost`, answers "what is Altune spending" from
two INDEPENDENT sources, each degrading on its own:

- **Provider API usage** — reads go-api's `GET /observe/metrics/live` (moved from
  `/admin/metrics/live` in #2805, gated to `OVERSEER_PRINCIPAL_ID`) `providers`
  field (the per-provider outbound-call counts built by the cost-enabler) via the read-only
  goapi client's new `AdminProviderUsage()` (`internal/goapi/cost_reads.go`). Renders a
  per-provider breakdown (ok / quota / error), idle providers dropped, plus a bounded usage
  trend. The read is **additive at file level** — its own file, never editing `client.go` —
  decoding a disjoint field (`providers`) from the same endpoint the Back-end performance
  bucket reads for latency, so neither read constrains the other and go-api adding counters
  won't fail either decode. It is registered on the client's observe-only allowlist
  (`observeonly_test.go`, `readOnlyMethods`), so the observe-only invariant cannot regress
  silently. Intake rides the shared `get` primitive's 1 MiB body cap.
- **OCI infra spend** — pulls current-period (month-to-date) cost from OCI's **usage-api**
  through a new read-only client (`internal/oci`) authenticated by **instance principal**
  (no stored key ever touches disk or config; the tenancy OCID is read from the same
  provider, never configured by hand). Renders the period total, top-5 per-service breakdown,
  and a bounded spend trend. The SDK is narrowed to a **single-method seam** (`usageAPI`, only
  `RequestSummarizedUsages`), so no mutating usage-api call can reach the client by
  construction. Off an OCI instance (dev, CI) the instance-principal handshake cannot succeed,
  so the half degrades to source-down; the client is built lazily on first collect (off the
  startup and HTTP-serve paths) and a failed build retries next tick.
- **Independent degrade** — one source down flags ONLY its own half `STALE` (last-known value
  preserved) while the other stays `LIVE`. `Collect` returns an error to the shell only when
  BOTH sources are unreachable, and even then `Render` still serves each half's last-known
  value flagged stale.
- **Bounded storage** — a separate `core.RingStore` per source, each capped at 120, so memory
  is bounded no matter how long the service runs or how hard collect is driven.

## How to invoke it

- Config (env):
  - Provider usage reuses the platform's go-api source — `OVERSEER_GOAPI_URL`,
    `OVERSEER_GOAPI_TOKEN` (operator bearer). Missing/invalid config degrades to a null reader
    (an invalid URL is logged so a typo is not mistaken for a real outage).
  - OCI spend is **off by default**; set `OVERSEER_OCI_ENABLED=1` (or `true`/`yes`/`on`) on the
    deployed OCI instance to activate it. Off the box it always degrades to source-down.
- HTTP: the panel renders inside the owner-only shell (`GET /`). No new route.
- Run locally: `cd services/overseer && OVERSEER_OWNER_TOKEN=<32+chars> go run ./cmd/overseer`
  (provider usage renders if go-api is configured; OCI spend renders STALE off the box).
- Gate (pinned toolchain — golangci crashes on Go 1.27): `GOTOOLCHAIN=go1.26.6`, then
  `go build ./... && go vet ./... && golangci-lint run --config ../go-api/.golangci.strict.yml ./...
  && CGO_ENABLED=1 go test -race ./... && govulncheck ./... && nilaway ./...`.

### Deployment TODO (load-bearing for the spend half)

OCI spend renders **STALE until the OCI instance is granted a usage-api read policy**. Beyond
setting `OVERSEER_OCI_ENABLED=1`, the instance's dynamic group needs an IAM policy allowing
the usage-api read, e.g.:

```
Allow dynamic-group <overseer-instances> to read usage-report in tenancy
```

Without that policy the instance-principal handshake succeeds but `RequestSummarizedUsages`
returns `NotAuthorizedOrNotFound`, which the bucket sanitises to `HTTP 404
(NotAuthorizedOrNotFound)` and degrades to stale — no crash, no leak, just an empty spend half
until the policy lands.

## Invariants it holds (spine + Cost-specific)

Confirmed on the assembled bucket at epic-close (green gate + a hostile whole-bucket attack):

- **read-only OCI, no write path** — the SDK seam `usageAPI` exposes exactly one method and it
  is the read `RequestSummarizedUsages`; no mutating usage-api call can reach the client
  because none is in the seam (`TestUsageAPISeamIsSingleReadOnlyMethod`, reflection guard). The
  bucket never modifies infrastructure.
- **observe-only go-api** — the only new go-api method is a GET on the read-only allowlist
  (`TestClientExposesOnlyReads`).
- **no OCI identifier leaks** — `Spend` is a narrow projection carrying only total, currency,
  period and per-service names — never a tenancy/compartment/resource OCID; the projection
  drops the OCIDs the usage-api items carry (`TestSpendCarriesNoOCIIdentifier`,
  `TestNoOCIIdentifierInRender`). A usage-api **service error is sanitised to just its HTTP
  status and service code** before it can reach a log (`sanitize()` in `oci/client.go`) — the
  free-form message, endpoint and opc-request-id, which can echo an identifier, are dropped
  (`TestServiceErrorSanitisedBeforeLog`).
- **two sources degrade independently** — OCI down flags only the spend half stale; go-api down
  flags only the provider half stale; only both-down returns an error, and even then both
  last-known values still render (`TestSpendDegradesIndependently`,
  `TestUsageDegradesIndependently`, `TestBothDownReturnsError`).
- **degrade-don't-crash** — an unconfigured source (off-box OCI, unset go-api) degrades to a
  null reader that reports source-down, never a startup crash; the lazy OCI reader degrades and
  retries on a build failure (`TestUnconfiguredDegradesNotCrashes`,
  `TestLazyReaderDegradesOnBuildFailure`).
- **bounded storage** — the two source trends are separate `RingStore`s capped at 120 by
  construction (`TestStaysBoundedUnderLoad`, driven 3× the cap).
- **render-escaping** — every external field (OCI service name, currency, go-api provider
  label, trend text) is HTML-escaped before entering `Panel.Body`; the LIVE/STALE lines are
  fixed literals and amounts are numeric-formatted. A hostile service name or provider label
  cannot inject markup (`TestRenderEscapesServiceName`, `TestRenderEscapesProviderName`).
- **read/render race-free** — `Render` copies the last-known pointers under the lock; the
  record helpers only ever REPLACE the snapshot (never mutate in place), so the two collects
  and the HTTP render run concurrently without a data race (`TestConcurrentCollectAndRender`,
  `go test -race` green).
- **additive** — `cost_reads.go` never edits `client.go`; the bucket owns all its files and
  self-registers with one blank import; core/shell/app depend on no concrete bucket.
- **owner-only** — the panel is reachable only through the owner-guarded shell.
- **no go-api internal imports** — the bucket reads go-api only over HTTP via the goapi client
  (module-wide guard `internal/guard/imports_test.go`).

## Hardened at epic-close (whole-bucket attack)

The whole-bucket attack found one real defect the per-leaf gates couldn't see — an interaction
between the OCI SDK's default circuit breaker and the `sanitize()` leakage defence; fixed here:

1. **Open circuit breaker leaked OCI identifiers into the collect-failure log (MEDIUM,
   secret/log-hygiene)** — `sanitize()` (`oci/client.go`) redacted only `common.IsServiceError`
   errors and passed every other error verbatim, on the assumption that a non-service error is a
   transport/dial failure carrying no identifier. But the usage-api client enables a **default
   circuit breaker** (SDK `usageapi_client.go:58`); a sustained usage-api outage opens it, and the
   SDK collapses the open breaker into a **plain `fmt.Errorf`** (`common/errors.go:294`) that
   embeds the request endpoint plus a history of the prior failures — `Opc-Req-id`, error code and
   the free-form service message, which can echo a tenancy/compartment/resource OCID. Not a service
   error, so it slipped past `sanitize` into the `SourceDownError` the shell logs
   (`app.go` `slog.WarnContext`), every open-window tick while both halves are down. Fixed by
   redacting the open-breaker error (`common.IsCircuitBreakerError`) and, as a deny-direction
   backstop, any non-service error still carrying an `ocid1.` / opc-request-id token, to a fixed
   non-identifying fact — genuine transport/dial errors stay verbatim for diagnostics. Regression
   guards added (`TestCircuitBreakerErrorSanitisedBeforeLog` reproduces the exact SDK open-breaker
   text; `TestTransportErrorKeptVerbatim` proves the redaction stays targeted); the guard was
   confirmed to fail against the pre-fix `sanitize` (leaked `ocid1.tenancy…`, `ocid1.instance…`,
   the opc-request-id and the endpoint).

Attacked and clean: the independent-degrade matrix (each/both/recovering), OCI mutating-call
reachability (reflected over the seam — none), the OCID-dropping `Spend` projection, provider+spend
render XSS (every dynamic write escaped), bounded cap-120 rings under a collect flood, races
between the two collects and render (`go test -race`, incl. stress), and shell survival on a
panicking bucket (`safeRender`/`safeCollect`/`safeStore`, spine-level).

Deferred (follow-up ticket): a LOW/cosmetic int64 overflow in the provider-usage aggregation
(`usageSignal` / `ProviderOutcomes.Total`) — display-only, HTML-escaped, on a trusted monotonic
source ~292k years from the int64 ceiling; not fixed in this slice.

## Not in this slice (roadmap)

Cost forecasting/alerting, per-request cost attribution, and any action on infra are all
out (read-only by charter). Remaining Overseer work per `notes/overseer.md`: Front-end health
→ Security → remove Mission Control.
</content>

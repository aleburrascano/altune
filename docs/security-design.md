# Security — design

Brief: `docs/security.md`. An **active** bucket (it makes requests), fenced hard.

## Anchor & inherited invariants
- **Anchor:** continuous proof the app rejects the attacks we hardened against.
- **Inherited:** probes-stay-home · no destructive probe · bounded · owner-only.

## Grounded in
- go-api's surface (`internal/app/routes.go`): `/health` open; `/v1/*` behind `auth.Middleware`
  (401/403); `/admin/*` operator-only; rate limits on discovery (`search_rate_limit.go`, 429).
  These are what the self-tests assert. The Overseer's `internal/goapi/` config carries the go-api
  base URL.

## Significance
**Significant** — introduces **active outbound probing** (new behavior), which is why the fence is
the load-bearing decision.

## Design decisions
- **Boundaries:** new bucket `internal/buckets/security/` with a **prober** component. Unlike passive
  buckets it needs a raw HTTP client (to send *unauthenticated* and *malformed* requests), so it does
  **not** reuse the operator `goapi.Client` — it has its own fenced client.
- **The fence (the crux):** a `validateTarget(host)` gate checked **before every request** against an
  **own-infra allowlist** (config: the configured go-api host(s); nothing else). A target off the
  allowlist is refused, not sent. Structural — the probe cannot construct a request to a
  non-allowlisted host. *Over:* trust the configured base URL implicitly — rejected, an explicit
  allowlist is the testable invariant.
- **The suite (safe checks only):** unauth `/v1/*` → 401/403; `/admin/*` as non-operator → 403;
  a burst → 429 (rate limit holds); a known-bad input on a read endpoint → rejected, not 500. **No
  POST that mutates data**; reads and rejection-assertions only.
- **Scheduling:** a ticker (default hourly, tunable via `OVERSEER_SECURITY_INTERVAL`), low volume;
  bound to app-lifetime ctx; recover-guarded (a panicking probe never crashes the shell).
- **Render:** pass/fail per check + last-run + any regression, bounded history.

## Architectural invariants (spine)
- **Probes-stay-home:** every probe target is validated against the allowlist; off-allowlist → refused. (Test: point it off-allowlist → no request sent.)
- **No state-mutating probe** — the suite only reads and asserts rejections.
- Scheduler goroutine is recover-guarded and ctx-bound (no leak, no crash).

## Slice
**Single leaf:** the fenced prober client + the check suite + the scheduler + the bucket + one
registration line. Plant: the fence test (off-allowlist refused), a no-mutation assertion, panic-containment.

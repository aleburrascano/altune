# Domain quality — design

Brief: `docs/domain-quality.md`. Platform: `docs/overseer-design.md`.

## Anchor & inherited invariants
- **Anchor:** search-quality score + acquisition success rate, at a glance.
- **Inherited spine:** bounded storage · observe-only · owner-only · degrade-don't-crash.

## Grounded in
- Sources (operator-only, confirmed): `GET /admin/eval` (`serveEval`, the eval-meter score) and `GET /admin/acquisition` (acquisition health) — `internal/admin/handler/admin_handler.go:79-86`. Overseer read client: `internal/goapi/`.

## Significance
**Extends the plugin pattern** (new bucket + one new goapi read file).

## Design decisions
- **Boundaries:** new bucket `services/overseer/internal/buckets/domainquality/`; read methods for `/admin/eval` + `/admin/acquisition` in a **new file** `internal/goapi/eval_reads.go` (additive; allowlist-registered).
- **What it renders:** the eval-meter score (vs baseline) + acquisition success rate; bounded recent history via `core.RingStore`.
- **Confirm at build:** exact JSON fields of `/admin/eval` and `/admin/acquisition` (read the handlers) before shaping the structs.
- **Degrade:** source down → last-known flagged stale.

## Architectural invariants (add to epic spine)
- The eval/acquisition reads live in their own `internal/goapi` file, never editing `client.go`.

## Slice
**Single leaf** (eval_reads.go + domainquality bucket + registration line). **Ready** (reads existing endpoints). Shares only the `cmd/overseer/main.go` registration line with the other bucket epics (additive import).

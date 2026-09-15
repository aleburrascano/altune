# Domain quality (Overseer bucket)

Idea brief. Platform: `docs/overseer.md`, `docs/overseer-design.md`. Bucket #6. Built on the plugin spine.

## Vision
Is the core product actually good right now — is discovery/search returning quality results, and is acquisition succeeding? This is the app's reason to exist, made legible to the owner at a glance.

## The idea
A Domain quality bucket that reads go-api's operator eval surface (`/admin/eval` — the in-process eval meter scored against a baseline) plus acquisition health, and renders a **search-quality score + acquisition success rate**.

**Rejected alternatives:**
- Recompute evals in the Overseer — rejected, go-api already scores them (evalmeter); the Overseer reads the verdict, it doesn't re-run the pipeline.
- Raw event mining — rejected, the eval meter is the purpose-built signal.

## Assumptions
- `/admin/eval` exposes the eval-meter score readably; acquisition health is readable via an operator endpoint. (Confirm exact fields in design.)

## Scope / non-goals
**In:** eval-meter quality score + acquisition success rate, rendered panel.
**Out:** deep per-query diagnostics; re-running evals (that's go-api's `/admin/rerun`); catalog analytics.

## Priority
**Must:** eval score + acquisition success rate. **Then:** trend, drill-down.

## Invariants
- Bounded storage · observe-only · owner-only · degrade-don't-crash.

## Open questions
Exact readable acquisition-health metric — confirm the endpoint/fields in design.

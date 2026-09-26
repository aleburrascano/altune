# Capability: Overseer — Domain quality, id-anchored discography suspects + suspect-rate headline

Epic #1799 (closed). Brief: `docs/features/domain-quality-deeper/shape.md` (what — the id-anchor +
headline delta). Design: `docs/features/domain-quality-deeper/design.md` (how). Extends the shipped
Discography category (epic #1425, `docs/features/domain-quality-deep/notes.md`) on the Overseer
platform spine (`docs/features/overseer/notes.md`). This note is the capability's memory, confirmed
live on prod at epic-close — not just merged.

Two children shipped this pass, both on `main` and auto-deployed: #1800 (tracer: id-anchor the
suspect signal end to end) and #1801 (windowed suspect-rate headline). No migration — pure code over
the existing `discovery_events` JSONB payload.

## What now works

Before this pass, the Discography bucket flagged a "contamination suspect" purely by **raw provider
headcount** — any release supplied by exactly one provider, full stop. That was wrong for a real
case: a single-provider release that carries a strong, verified shared id (ISRC/MBID) is not actually
suspect, it is just under-covered by the fan-out; the id already proves which recording it is.

Now the suspect signal is **id-anchored**: a release counts as a real suspect only when it is
single-provider **and** carries no strong/verified id (`single_provider_no_id`). An id-verified
single-provider release drops out of the worst-first ranking entirely; a no-id single-provider release
stays top-ranked. Headcount is consulted only as the fallback, for releases with no id information at
all — never as the primary signal, and no provider is ever treated as ground truth or given a
hand-set trust weight.

On top of that, the panel now shows a **windowed suspect-rate headline**: the share of real
discography opens (within the query window) whose worst release-suspect fired, so the owner gets a
single "how bad right now" number instead of only a list of cases. It is computed over real
production `discography_observed` events only — an eval or synthetic run never emits that event, so
it can never move the rate — and it carries the age of its last real sample beside it, so a dead feed
reads as stale, never as a falsely-live zero.

## How to reach it

- **Owner path:** open the Overseer at `/overseer/` (owner-only, per `docs/features/overseer/notes.md`)
  → the Domain quality panel → the **Discography · worst first** list. Each row now reads
  "`N`/`M` single-provider, `K` without a shared id" (the id-backing evidence), ordered exactly as
  go-api ranked it — the panel renders the served worst-first order, it never re-sorts client-side.
  The headline above the list shows the windowed **suspect rate** plus the age of the last real
  sample it was computed from.
- **Operator API (go-api, what the Overseer reads):**
  `GET /observe/quality/discography?window_days=<n>&by=<artist|provider|contamination_band>`
  (moved from `/admin/quality/discography` in #2805; gated to `OVERSEER_PRINCIPAL_ID`, unchanged
  route shape from #1425). The response now additionally carries, per case,
  `single_provider_no_id` (the id-anchored suspect count) alongside the existing `single_provider`
  (raw headcount) and `provider_counts`, ranked worst-first by
  `single_provider_no_id / releases`; and a top-level `suspect_rate` (0..1) plus its window — the
  windowed headline.
- **What computes it:** `MergeReleases` already stamps `HasStrongID` / `IDVerified` on every
  `MergedRelease` (`services/go-api/internal/discovery/service/release_merge.go`); the
  `discography_observed` emit now copies those into the payload as `single_provider_no_id`
  (`services/go-api/internal/discovery/service/discography_telemetry.go`) alongside the unchanged
  provider counts — same best-effort, recovered, off-the-hot-path emit as before, one more
  field-copy, no new outbound call. The aggregate SQL
  (`services/go-api/internal/discovery/adapters/persistence/event_repo.go`) ranks by the no-id ratio
  and computes the windowed `SuspectRate` over the same table.

## Must-holds it keeps (confirmed live)

Inherits every rule from the base Discography slice (#1425: observe-only, disagreement-is-a-hint,
bounded storage, windowed + anonymized data, owner-only, independent degrade, render-escaping,
additive, verdict computed in go-api never re-run in the Overseer, best-effort off-hot-path emit).
This pass adds, and keeps live:

1. **Id is the anchor, headcount is the fallback** — a suspect is decided by `HasStrongID`/
   `IDVerified` first; plain headcount is used only when no id info exists. Proven by test: an
   id-verified single-provider release is not top-ranked; a no-id single-provider release is.
2. **No source is crowned truth** — no code path (go-api scoring or Overseer render) treats any
   single provider as ground truth.
3. **No hand-set trust weights** — no static per-provider weight constant anywhere in the score
   function or config.
4. **Suspect rate counts real requests only** — computed over windowed `discography_observed` rows,
   which only the live discography-open path emits; an eval/synthetic run is proven (test) not to
   move it.
5. **Freshness shown, never faked** — the suspect-rate headline carries the age of its last real
   sample; a past-window slice still renders STALE with the last-known value, inherited from the base
   design's independent-degrade.

## What broke before (caught during this build, now fixed and tested)

- **The panel re-ranking bug.** While wiring the new field through, the Overseer panel initially kept
  its own client-side sort by *raw single-provider headcount* instead of rendering the worst-first
  order go-api now serves — which silently reintroduced the exact bug this epic exists to kill: an
  id-verified single-provider release could still be rendered as the top suspect on the visible
  surface, even though go-api's aggregate correctly ranked it low. Fixed in commit
  `89fd1817` (`fix(adapters): rank domain-quality panel by served no-id order`): the panel now renders
  the served worst-first order verbatim (never re-sorts) and grades each row's percentage/severity by
  the id-anchored `single_provider_no_id / releases` ratio, so the shown score matches the shown
  order. A regression test asserts a divergent case (headcount order != no-id order) renders the
  no-id-ratio order, not headcount order.

## Where it runs

- **go-api enabler** (`services/go-api/internal/discovery/service/discography_telemetry.go`,
  `internal/discovery/adapters/persistence/event_repo.go`, `internal/discovery/ports/ports_telemetry.go`,
  `internal/observe/handler/quality.go`): same `GET /observe/quality/discography` route as the
  base slice (moved from `/admin/quality/discography` in #2805), gated to `OVERSEER_PRINCIPAL_ID`,
  extended additively — no new table, no new endpoint, no migration (pure
  code over the existing `discovery_events` JSONB payload).
- **Overseer reader** (`services/overseer/internal/goapi/quality_reads.go`,
  `services/overseer/internal/buckets/domainquality/domainquality.go`,
  `services/overseer/web/src/panels/domainquality.panel.tsx`): version-skew tolerant read (an older
  go-api without `single_provider_no_id` decodes to zero rather than failing), renders the extended
  evidence and the suspect-rate headline inside the existing Discography block.
- **Single deployment target:** merge to `main` auto-deploys via `.github/workflows/deploy-backend.yml`
  (blue-green) to the OCI prod VM. Confirmed live on prod at
  **https://altune.duckdns.org**, running commit `b96df338` (== `main`), verified after ship's
  blue-green deploy + Overseer container rebuild, with prod smoke passing functionally. Staging
  mirror: https://altune-staging.duckdns.org.

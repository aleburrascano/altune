# Capability: Overseer — Domain quality deepening (Discography category)

Epic #1425 (closed). Brief: `docs/features/domain-quality-deep/shape.md` (what — the 7-category
vision). Design: `docs/features/domain-quality-deep/design.md` (how). Built on the Overseer platform
spine (`docs/features/overseer/notes.md`) and the shipped bucket #6 (`docs/features/domain-quality/notes.md`).
This note is the capability's memory, confirmed live on prod at epic-close — not just merged.

Four children shipped this pass, all on `main` and auto-deployed:
#1426 (tracer: emit + endpoint + read + panel), #1427 (worst-first aggregate + `by=` group-on-demand),
#1429 (full render: evidence + trend + STALE + pivot), #1428 (windowed retention/prune).

## What now works

The brief names **seven** quality categories along the user's search → discography → acquisition
journey (query understanding, search ranking, coverage, identity/matching, discography, acquisition,
provider health). **Only the first — Discography / detail correctness — is built this pass.** The
other six remain named slots on the spine, each its own future slice (see design's deferred list).

Discography, live: when a real user opens an artist's discography, the platform already fans out to
multiple providers and merges their releases (`MergeReleases`). That merge already computes, per
release, which providers agreed on it. This capability records that disagreement on every real
request and lets the owner see it: an artist whose page mixes tracks providers disagree on
(contamination suspects) or where one provider's catalog gaps behind another's (incompleteness)
now surfaces on its own, worst-first, with the provider-by-provider counts attached — no one had to
predeclare "watch Radiohead" or "watch shared-name collisions" for it to show up.

The owner opens the Overseer, and the Discography block of Domain quality (bucket #6) shows:
- the worst real cases first (highest contamination/imbalance), each with `releases`,
  `single_provider` (contamination-suspect count) and `provider_counts` per provider — the evidence
  to go judge the case, not a verdict;
- a bounded trend of the top contamination ratio over time;
- three groupings rendered together — by artist (default), by dominant provider, by contamination
  band — so a pattern (e.g. one provider driving most suspects) can show itself without the owner
  having named that cohort up front;
- STALE (with the last-known value, never blank) if go-api's read fails, independent of the eval and
  acquisition blocks in the same panel.

**Live proof at epic-close (prod):** owner panel `GET /overseer/` → 200, Domain quality live (score +
success rate, no STALE); operator `GET /admin/quality/discography` → 200
`{"window_days":30,"group_by":"artist","cases":[...]}` (this route moved to
`/observe/quality/discography` in #2805); opening Radiohead's discography
(`/v1/discovery/artists/spotify/4Z8W4fKeB5YxbusRsdQVPb/albums`) emitted a `discography_observed`
event that surfaced as a worst-first case (`"releases":43,"single_provider":43,"provider_counts":
{"spotify":43}`, i.e. this artist's set is currently backed by one provider only); the Overseer panel
then rendered that case live in the Discography block.

## How to reach it

- **Owner path:** log into the Overseer → `/` (owner-only, cookie or bearer per
  `docs/features/overseer/notes.md`) → the Domain quality panel → the Discography block.
- **Operator API (go-api, what the Overseer reads):**
  `GET /observe/quality/discography?window_days=<n>&by=<artist|provider|contamination_band>`
  (moved from `/admin/quality/discography` in #2805; gated to `OVERSEER_PRINCIPAL_ID`).
  `window_days` defaults to 30, clamps to `[1, 365]` (a hostile/fat-fingered value
  is clamped, never echoed). Response: `{"window_days","group_by","cases":[{"artist","artist_ref",
  "releases","single_provider","provider_counts","last_seen"}]}`, worst-first, capped at 200 cases.
  Bounded 5s query timeout → coded 504 rather than a parked request.
- **What emits the signal:** `GetArtistContentService.GetAlbums` (v2 fan-out path,
  `internal/discovery/service/get_artist_content.go`), after `MergeReleases`, appends a
  `discography_observed` event (`internal/discovery/domain/events.go`) via the injected
  `ports.EventStore` — best-effort, off the response path, recovered on panic. Persisted in the
  existing `discovery_events` table (no new table).
- **Retention:** a background job (`services/go-api/internal/app/background_jobs.go`,
  `startDiscographyPrune`) runs every 24h and evicts `discography_observed` rows older than 400 days
  (`discographyRetentionWindow`, `event_repo.go`) — deliberately wider than the 365-day max
  `window_days` a caller can request, with margin for clock skew, so the prune can never remove a
  row a legitimate query could still read. Only this event type is pruned; every other
  `discovery_events` type owns its own retention (see follow-ups).

## Where it runs

- **go-api enabler** (`services/go-api/internal/discovery/...`, `internal/observe/handler/quality.go`):
  emit → persist (existing `discovery_events` + `EventStore.Append`) → windowed/grouped aggregate
  query → `GET /observe/quality/discography` (moved from `/admin/quality/discography` in #2805),
  gated to `OVERSEER_PRINCIPAL_ID`, alongside the existing `/observe/eval` and
  `/observe/acquisition`.
- **Overseer reader** (`services/overseer/internal/goapi/quality_reads.go`,
  `services/overseer/internal/buckets/domainquality/domainquality.go`): an additive, allowlisted read
  plus the Discography category render inside the existing bucket #6. It never re-fetches providers
  or re-runs `MergeReleases` — it renders the verdict go-api already computed.
- **Single deployment target:** this repo has no staging tier. Merge to `main` auto-deploys via
  `.github/workflows/deploy-backend.yml` (blue-green) straight to the OCI prod VM
  (`altune.duckdns.org`). All four children shipped this way; there was no separate "promote to prod"
  step.

### Operational fact for the runbook: the Overseer needs an operator credential in prod

The Overseer's bucket reads (Domain quality and every other bucket) run against go-api's
operator-only surface. In prod this requires the refresh-token trio —
`OVERSEER_SUPABASE_URL`, `OVERSEER_SUPABASE_ANON_KEY`, `OVERSEER_GOAPI_REFRESH_TOKEN` — set in the
VM's `services/go-api/.env.production`. **This file is manual and secret-bearing; it is not
committed and merging code does not populate it.** Without it, the Overseer has no working token
source and every bucket (not just Discography) renders **STALE**, independent-degrade doing its job
correctly but silently — it looks like "nothing is live" rather than "no credential configured."
This was exactly the state found at epic-close: the trio was written to the VM's `.env.production`
and the `altune-overseer` container recreated; the Overseer then logged
`token source selection mode=refreshing` and every bucket went live, Domain quality included. Any
future prod redeploy of the Overseer container that loses that env file will silently regress to
all-STALE — check the container's startup log line for `mode=refreshing` (vs `mode=static` /
`mode=null`) after any redeploy.

## Must-holds it keeps (confirmed live)

Inherited from the platform + bucket #6 spine, extended for production data by this pass. QA verdict
was green — all 8 held, 3 seams clean, usable gate passed:

1. **Observe-only / no-act** — no path mutates the watched app or re-runs a pipeline; the enabler
   endpoints are reads only; the Overseer client exposes no mutating method (allowlist-checked).
2. **Disagreement is a hint, never a verdict** — a case renders labelled "suspect" with its provider
   evidence; no code branch treats it as ground truth.
3. **Bounded storage, always** — `discovery_events` is capped by the 400-day prune; the Overseer's
   own trend/pivot state is a fixed-capacity `RingStore`/map, never unbounded.
4. **Production data windowed + anonymized** — the aggregate reads a fixed, clamped window
   (`[1,365]` days); the `discography_observed` payload carries artist + provider counts + timestamp
   only, no user identity, raw or hashed.
5. **Owner-only** — the bucket render sits behind the Overseer owner guard; the enabler endpoint is
   gated to `OVERSEER_PRINCIPAL_ID` (`observeHandler.Gate`).
6. **Independent degrade, don't crash** — the Discography block STALEs on its own read failure,
   independently of the eval/acquisition blocks in the same panel; last-known value is kept, never
   blanked.
7. **Render-escaping** — every rendered field (artist ref, provider names) is HTML-escaped before
   entering the panel body; production query/identity text is never rendered raw.
8. **Additive** — its own files on both sides plus one registration point each (the admin route, the
   Overseer bucket's blank import); no edit to the Overseer read client core or another bucket.

Plus the two boundary rules from design: **the verdict is computed in go-api, never re-run in the
Overseer** (the reader has no provider/merge call), and **recording is best-effort and off the hot
path** (a panic in the emit is recovered and dropped; the artist response is never slowed or failed).

## What broke before / gaps QA and build surfaced (now open follow-ups, not fixed here)

- **#1434** — retention/prune was built only for `discography_observed`; the other `discovery_events`
  types still lack a bounded-window prune of their own (each owns its own retention by design, but
  most haven't gotten one yet).
- **#1435** — the `last_seen` field in the discography-case payload is currently dead: computed and
  served but not read by any consumer yet.
- **#1436** — `TestWriteDeadline` is flaky (unrelated to this feature's logic; flagged during build).
- **Pivot is render-all, not click-toggle.** The "group-on-demand" pivot (`by=artist` /
  `provider` / `contamination_band`) currently re-reads and renders **all three groupings on every
  collect cycle** rather than the owner clicking to switch views live — that needs an Overseer shell
  change (a UI toggle) that wasn't in scope this pass. The engine (per-grouping live reads) is real;
  only the display is render-all today.

None of these block the Discography category being live and correct; they are scoped as their own
tickets, not reopened against this epic.

## Not in this slice (the other six categories, per the brief's roadmap)

Coverage (cheapest next — mostly exposing existing zero-result / `CoverageReportA` data) → Search
ranking → Query understanding → Identity/matching → Acquisition + audio quality → Provider health as
a quality driver. Each is a new category on the same enabler + thin-reader spine this slice proved.
Also parked, by design: auto-clustering/emergent group naming, true-language + rising-artist signals,
user-behavior as a second truth source alongside cross-provider disagreement.

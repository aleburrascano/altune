# Domain quality — deeper — design

Brief: `docs/drafts/domain-quality-deeper.md`. **Base design it extends:**
`docs/drafts/domain-quality-deep-design.md` (the Discography slice-1 enabler + thin reader).
Platform: `docs/drafts/overseer-design.md`. Enabler precedent: `docs/drafts/metrics-enabler-design.md`.

Design settles the *how*, read against the real code; it does not re-open the *what*. This design
does **not** restate the base design — it records only the deltas the deeper brief forces on the
same slice-1 (Discography).

## Inherit

- **Goal:** show where the catalog looks wrong and how sure we are; turn "search feels off" into an
  evidenced worklist. Never stamps "right".
- **Known must-holds (deeper):** id is the anchor / headcount is fallback · no source crowned truth ·
  no hand-set trust weights · freshness shown never faked · suspect rate over real requests only.
  Plus everything the base design already carries (observe-only, best-effort emit, no user id,
  bounded window, server-emitted only, owner-only, render-escaping).

## Grounded in (real code) — only the delta the base design didn't lean on

- **The id-anchor is already computed at the merge point.** `MergedRelease` carries `HasStrongID` and
  `IDVerified` alongside `Providers` (`internal/discovery/service/release_merge.go:15-18`), set per
  release during `MergeReleases` via `hasStrongID(variant)` and `group.IDVerified` (`:41-46`). So the
  signal the deeper brief wants — "is this attribution backed by a shared id?" — is a field that
  exists at t=0, not a new computation.
- **Results carry the ids themselves:** `SearchResult.ISRC`, `.MBID`, `.Xref`
  (`internal/discovery/domain/search_result.go:20-23`); resolution basis enum
  `EntityResolutionISRC` / `EntityResolutionMBID` (`domain/enums.go:72-83`). The upstream stamping
  (`service/identity_stamp.go`, durable identity store, MBID index, xref bridging) is what fills
  them, with real-but-partial coverage — which is exactly why headcount stays as the fallback.
- Everything else the slice needs (fan-out at `GetArtistContentService.GetAlbums` v2,
  `EventStore.Append`, `EventQuery` JSONB aggregates, `/admin/*` `OperatorOnly`, the Overseer plugin
  + `RingStore` + observe-only allowlist) is already grounded in the base design; unchanged.

## Significance

**Extends the base design's slice-1** — same enabler + thin reader, no new infra, no new service, no
new store. It forces one sharpened decision (record and rank by the id-anchor, not raw headcount) and
three render additions (suspect rate, drift line, freshness age). Walk only the lenses that change.

## The shape (delta over the base flow)

```mermaid
flowchart LR
  M[MergeReleases\nProviders + HasStrongID + IDVerified\nALL already computed] -.async, best-effort.-> E[emit discography_observed\npayload NOW carries per-release:\nprovider count + HasStrongID + IDVerified]
  E --> DB[(discovery_events\nwindowed + pruned)]
  DB --> Q[EventQuery aggregate\nsuspect score = single-provider AND no strong id first;\nid-verified single-provider = low suspect;\nheadcount only when no id info\n+ window suspect rate]
  Q --> EP[GET /admin/quality/discography\nOperatorOnly]
  EP ==operator bearer==> R[Overseer goapi/quality_reads.go]
  R --> B[domainquality bucket · Discography\nworst-first + provider/id evidence\n+ suspect-rate headline\n+ drift line from bounded RingStore\n+ per-slice freshness age / STALE]
```

## Design decisions (only the lenses that change)

**Data & payload — record the id-anchor, not just the count.** The `discography_observed` payload
(base design) gains, per release: `has_strong_id` and `id_verified` beside the existing provider
count. All three are read straight off `MergedRelease` at the point the base design already emits —
**no new hot-path work**, one more field-copy in the same map summarization.
- *Over:* record only `len(Providers)` (the base design's first cut) — rejected by the deeper brief:
  a single-provider release with a verified shared id is *not* a contamination suspect, and flat
  headcount would flag it. Recording `HasStrongID`/`IDVerified` is what makes the id the anchor.
- *Over:* record the raw ISRC/MBID strings in the payload — rejected: not needed for the verdict
  (the boolean id-backing is enough to rank), and it bloats the event and risks leaking identifiers.

**Ranking — the suspect score lives in the go-api aggregate (analysis next to the data).** The
`EventQuery` worst-first order becomes: **single-provider AND no strong id → top suspect; large
per-provider album-count gap → incompleteness suspect; single-provider WITH `id_verified` → low /
not suspect. Plain headcount is consulted only for releases with no id info at all.** Emitted as a
hint with its evidence, never a "wrong" verdict.
- *Over:* rank in the Overseer — rejected, same reason as the base design: analysis stays next to the
  data; the bucket renders the served verdict.
- **No hand-set weights, no crowned source:** the score is a function of the id fact + provider set,
  with no static per-provider trust constant anywhere. Emergent source reputation is a **later
  slice**, not built here.

**Suspect rate (headline).** Computed in the same aggregate as the share of windowed
`discography_observed` events whose top release-suspect fired, over real production opens only (eval
/ synthetic traffic never emits this event, so it cannot move the rate). Served as one number on the
endpoint.

**Drift.** The Overseer bucket already keeps a bounded `RingStore` of served snapshots (base design);
the deeper brief promotes it to a first-class **trend line** in the Discography panel and flags a
clear step-change. Pure render + the existing ring — no new store, no go-api change.

**Freshness.** Each rendered slice shows the **age of its last real sample**; on source-down it
renders **STALE with last-known age** (inherited independent-degrade, made explicit in the render so
a dead feed can never read as a live number).

**Acquisition is NOT in this slice.** The deeper brief's second family (file/fingerprint checks) is a
different engine and a later slice; this design covers only the id-anchored discovery engine on
Discography. Named here so ticketize does not pull it in.

## Architectural invariants (add to the epic core rules, on top of the base design's)

- **Id is the anchor, headcount is the fallback.** A discography suspect is decided by
  `HasStrongID`/`IDVerified` first; headcount is used only when no id info exists. (Test: an
  id-verified single-provider release is *not* top-ranked as a suspect; a no-id single-provider
  release is.)
- **No source is crowned truth.** No code path treats any single provider as ground truth. (Test: no
  provider short-circuits the score to "correct".)
- **No hand-set trust weights.** No static per-source weight table in config or code. (Test/lint: the
  score function references no per-provider constant.)
- **Suspect rate counts real requests only.** (Test: an eval / synthetic run does not move the rate.)
- **Freshness shown, never faked.** Every rendered slice carries its last-sample age; a past-window
  slice renders STALE. (Test: a stale slice renders STALE with age, not a live value.)

## Functional success signal (for ship)

No new service or deploy step (extends go-api + the existing Overseer). The marker that proves the
feature works, not merely booted: **`GET /admin/quality/discography` returns a real worst-case list
with per-release id-backed evidence after real discography opens, and the Overseer Discography panel
renders it with a suspect-rate headline.** ship gates on the panel showing real served data, not on
process liveness.

## Slice-1 build (what ticketize cuts) — delta over the base design's list

The base design's slice-1 steps stand. The deeper deltas fold in as:

go-api enabler:
1. `discography_observed` payload **carries `has_strong_id` + `id_verified` per release** (read off
   `MergedRelease`), in addition to the base design's provider counts. Same best-effort/recovered
   emit, same event type.
2. `EventQuery` aggregate ranks by the **id-anchored suspect score** (id-verified single-provider =
   low; no-id single-provider = top), and computes the **windowed suspect rate**.
   Plant (on top of base tests): id-anchor ranking test, no-crowned-source test, no-static-weights
   test/lint, suspect-rate-ignores-eval test.

Overseer thin reader:
3. `quality_reads.go` reads the extended endpoint (id evidence + suspect rate) — version-skew
   tolerant.
4. Discography panel renders: worst-first with **provider + id-backing evidence per row**, the
   **suspect-rate headline**, the **drift line** from the bounded ring, and **per-slice freshness
   age / STALE**. Every field HTML-escaped.

## Deferred (own later slices, per the deeper brief)

Acquisition + audio quality (file/fingerprint engine — different instrument) · drift across further
slices · the remaining discovery categories · provider-health-as-quality · emergent source reputation
· the eval-corpus promote loop (needs the owner-declares-the-answer flow + corpus format) ·
user-behavior as a second truth source.

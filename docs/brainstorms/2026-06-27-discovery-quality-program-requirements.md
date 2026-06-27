# Discovery Quality Program — requirements

Created 2026-06-27. Status: brainstorm (scope locked: all workstreams in). Spine
per user: "build everything." Sequencing below is **dependency-driven, not
priority** — everything is in scope.

## Framing

Discovery is a layered system. We've sharpened the **middle** (coverage → merge →
rank) to a strong bar; that exposed the **edges** (query-understanding in,
enrichment out) and the **cross-cutting** layers (consistency, observability) as
the new weak links. The organizing thesis: **observability is the multiplier** —
it turns anecdotal bug-spotting ("I noticed che's discography was off") into
visible trends + replayable traces, and feeds them into the eval harness as
regression cases. Build the flywheel, and every other layer's fixes get faster and
verifiable.

All gaps below are verified against the code this session.

## Workstreams

### 1. Consistency — app-wide result cache
- **Gap.** Same query returns different results to different people / on repeat
  within seconds. Root causes (verified): provider drop-out (1500ms timeout →
  provider excluded from that run's merge, `service/search.go` fanOut) and
  identity-bridge cache warmth (`stampIdentities` merges by name when cold, by
  identity when warm). No query-result cache exists today.
- **Scope.** A **shared, app-wide** (not per-user) short-TTL cache of the final
  ranked output keyed by normalized query. Discovery results are catalog-derived,
  not user-specific, so sharing is correct and is the point.
- **Success.** Identical normalized query returns identical results for everyone
  within the TTL window. The cache entry doubles as the canonical "what this query
  returned" record for tracing (feeds #4).
- **Open decision.** TTL (default proposal: 30–60s) and whether to cache partial
  (provider-degraded) results or only complete ones.

### 2. Query understanding — phonetics + alias/handle recall
- **Gap.** Two distinct recall failures at the front door:
  - **Phonetics:** `"sombr"` surfaces the artist; `"somber"`/`"sombre"` do not,
    though identical in sound. A full metaphone engine exists
    (`service/metaphone.go`) but is wired **only** into the vocabulary/correction
    layer, and correction fires **zero-results-only** — so a non-empty-but-wrong
    result set (the common case) never triggers it.
  - **Stylized handles / aliases:** underground names (`$NOT`, `che cxo`, stylized
    spellings) fail recall for reasons phonetics won't fix — they need alias/AKA
    resolution, a separate mechanism.
- **Scope.** Un-gate phonetic matching from zero-results-only so it can recover the
  intended entity even when wrong results exist; add alias/handle resolution.
- **Success.** `"somber"`/`"sombre"` surface `"sombr"` in top-K; known stylized
  aliases resolve to their canonical artist. **Eval-gated** — must not pollute
  clean queries with sound-alikes; same-sample A/B on exact + hard corpora.
- **Open decision.** How aggressive phonetic expansion is (recall vs precision),
  resolved via the eval.

### 3. Enrichment / discography correctness
- **Gap.** Artist discography (albums/EPs/singles) renders out of chronological
  order and missing year. Verified: no year-sort in `service/consensus.go` or
  `service/find_related.go`; order is provider-return order, year is whatever
  providers supply.
- **Scope.** Chronological ordering of the discography; fill year/metadata
  completeness from the best available provider (the enrichment merge already
  pulls MB/Discogs/Last.fm/Deezer).
- **Success.** `"che"` (and any artist) discography lists albums/EPs/singles in
  chronological order with year populated wherever any provider has it.

### 4. Observability platform extension (the flywheel)
- **Gap.** Telemetry records `search_performed` only. Detail-screen / discography
  fetches emit nothing; the `/events` endpoint exists but no behavioral events
  (play/click/library_add) are landing; no way to re-run a query from the
  Mission Control UI or trace a real user search to what it returned.
- **Scope.**
  - **Telemetry floor (prereq for the rest):** emit events for detail-open /
    discography fetches (albums/EPs/singles), and make behavioral events
    (play/click/library_add) actually land.
  - **Per-trace provider health:** surface which providers answered vs timed out
    for each query (makes #1's determinism work visible + measurable).
  - **In-UI re-run:** run an arbitrary query from the Mission Control page and see
    the full pipeline (the `discoverytrace` per-stage view, in the browser).
  - **Real-search tracing:** browse what users searched and trace each back to what
    actually came up (search → results → behavioral outcome).
  - **Discography traces:** see the extra album/EP/single fetches a detail-open
    triggers (where #3's ordering bug would show).
- **Success.** Any real or hand-entered query can be replayed and inspected
  stage-by-stage in the UI; user search history is browsable with outcomes;
  detail-fetch behavior is visible.

### 5. The eval flywheel (closes the loop)
- **Scope.** Observed bug/trend in the platform → captured as a `discoveryeval`
  regression case. First instance ready today: the **"noise-in-top-5"** tail-quality
  metric invented for the tail-noise demotion A/B should become a tracked
  `discoveryeval` signal Mission Control surfaces over time.
- **Success.** Each significant bug found via observability leaves behind a
  regression case, so it can't silently return.

## Dependency-ordered build sequence

1. **Telemetry floor** (#4 prereq): detail/discography + behavioral event emission;
   provider-health per trace. Unlocks everything observable.
2. **Consistency cache** (#1): independent, quick; its entry also becomes the
   trace record.
3. **Discography correctness** (#3): small, independent; now visible in traces.
4. **Observability UI** (#4): in-UI re-run + search/detail trace views, on the
   telemetry floor.
5. **Query understanding** (#2): biggest algorithm change, eval-gated, measured via
   the now-built observability + tail-quality metrics.
6. **Flywheel wiring** (#5): promote tail-noise-in-top-5 (and new) signals into
   `discoveryeval`; standardize bug→eval-case capture.

## Out of scope / deferred
- Per-user personalized ranking (discovery is intentionally not personalized).
- Re-recording the demotion eval against clean fixtures + flipping
  `TAIL_DEMOTION_ENABLED` — tracked separately in
  `docs/brainstorms/2026-06-27-discovery-tail-noise-demotion.md`, ships with this
  backend push.

## Cross-cutting note
Behavioral telemetry (play/click/library_add) is the **floor** the whole flywheel
stands on — without "what did people pick," the platform shows queries but not
outcomes, so it can't tell good results from bad at scale. It's step 1 for a reason.

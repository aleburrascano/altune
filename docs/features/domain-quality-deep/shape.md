# Domain quality — deepening (production-fed quality observability)

Idea brief for a major deepening of Overseer bucket #6, Domain quality. The shipped bucket
(`docs/features/domain-quality/`, epic #1347) renders two numbers: a 5-query eval score and an
acquisition success rate. This brief expands it into a production-fed quality lens over the whole
domain. Platform: `docs/overseer.md`, `docs/overseer-design.md`. Built on the plugin spine.

This brief shapes the **spine + the 7-category skeleton** and specifies the **first slice** deeply.
Per-category signal depth grows over time (living bucket); each category earns its own deeper pass
when its turn comes, exactly as the platform's buckets do.

## Vision, what it's for: the problem, the user, the outcome

The core product is: a user types a query, gets results, picks one, opens its page, plays/acquires
it. That is the app's reason to exist. Today the owner has no way to see, at a glance, **whether
that journey is actually good right now**, and when it is not, no way to see **why** without hand-
digging through logs and the CLI eval harness.

The problem: these algorithms (search, matching, acquisition) will never be perfect. There will
always be edge cases nobody designed for — other languages, niche artists, up-and-coming artists,
name collisions. The owner does not want to enumerate those cases up front (that list is endless
and brittle). The owner wants the platform to **surface real failing cases on its own, each
carrying enough signal to act**, so a pattern of edge cases points at the one change that hardens
the algorithm for the whole class.

Outcome wanted: open the Domain quality bucket and the current quality problems **pop out** —
the failing real cases, grouped so the weak spot is obvious, with every hint needed to go fix it
already attached. No time wasted figuring out *why*. The bucket observes and hints; fixing is a
separate job.

Why now: the owner is actively investing in the Overseer as a god's-eye platform, and this bucket
is the "is the product actually good" lens — the deepest business logic of all eight buckets.

Concrete motivating example (owner's words): search correctly finds an artist, but their
discography page is wrong — some tracks are not theirs (shared names), and some of their real work
is missing. The platform should make that visible with the signals to act, without anyone having
pre-declared "watch for discography contamination."

## The idea, the proposal, the reasoning, and the alternatives weighed and rejected

Deepen the bucket into **seven categories**, discovered by walking the user's journey through the
domain. Quality can break at each step; each break is a category. Each category is a container that
grows its own signals over time (an empty signal is a visible gap).

The seven categories (journey order):

1. **Query understanding** — the typo-corrector "fixes" a real niche name into a popular wrong
   one, or fails to fix a real typo. Signals: `CorrectedQuery`/`OriginalQuery`, `filtered_as_typos`,
   script class. A *silent* killer: a bad correction looks like a successful search.
2. **Search ranking** — the right thing exists but ranks low, or the wrong thing sits on top.
   Signals: zero-result, no-click, abandoned, match position / MRR, `FailuresByTopKind`.
3. **Coverage** — the thing is systematically absent. **This is where languages / niche /
   up-and-coming actually surface** as emergent clusters, without being named up front. Signals:
   zero-result query lists, `CoverageReportA` (strong/weak/abandoned/filtered-as-typos).
4. **Identity / matching** — same track from two providers not merged, or two different tracks
   wrongly merged; wrong artist attribution at the source. Another silent killer. Signals:
   `HasIdentifier` (ISRC/MBID), cross-provider agreement, artwork confidence / resolution tier.
5. **Discography / detail correctness** (the catalog axis, pulled back into scope) —
   contamination (tracks not theirs) + incompleteness (real work missing). Signal: cross-provider
   disagreement over the artist's set.
6. **Acquisition + audio quality** — did not get it, or got a bad file. Signals: reason codes,
   per-stage rejection breakdown, ffprobe/ffmpeg/fpcalc/fingerprint verification.
7. **Provider health as a quality driver** — a provider degrading silently poisons search *and*
   discography at once. The domain-quality question is "which source is dragging result quality
   down", not "which source is slow" (that is backend-perf). Signals: `/admin/providers` status,
   error rate, circuit-open.

**The universal engine (the spine under all seven).** Rather than pre-naming cohorts to watch, the
platform: (a) records **rich per-case signal from real production requests**, privileging no
dimension; (b) **surfaces the worst real cases**; (c) lets the owner **group on demand** by any
recorded field, so the pattern shows itself. The primary "is this wrong" signal is **cross-provider
disagreement** — structural, needs no ground-truth label, and fires on every real request:

- A result all providers agree on is safe; a result one provider alone returned (and the user
  ignored) is a weak-result suspect.
- On a discography, tracks only one provider attributes to the artist are contamination suspects;
  a large gap between providers' album counts is an incompleteness suspect.

Disagreement is a **hint, not a verdict** — it points the owner at a case to judge, it never
asserts "this is wrong."

**This is two features, not one.** Almost every signal above is already *computed* inside go-api
but not *served* — the rich sliced eval, the acquisition reason codes and rejection stages, the
zero-result / coverage-gap lists, the per-provider fan-out results all live in the offline CLI
harness or internal structures with no admin endpoint. So the work splits:

- **A domain-quality enabler in go-api** that records the cross-provider per-case signal on real
  requests and serves the aggregates + worst-case lists over operator-only endpoints. This is the
  real work. (Mirrors the existing metrics-enabler → backend-perf pattern.)
- **A thin deepening of the Overseer bucket** that reads those verdicts and renders them. The
  Overseer stays observe-only; it renders verdicts, it never re-runs a pipeline.

The exact enabler endpoint surface is `design`'s call; the *decision that there is an enabler* and
that the bucket stays a thin reader is settled here.

**Alternatives weighed and rejected:**

- *Pre-name the cohorts to watch (script, popularity band, genre, language) and slice by them.*
  Rejected: the owner explicitly does not want to enumerate an endless, brittle list. Naming
  cohorts up front bakes in today's guesses; the universal engine lets tomorrow's unknown edge
  case surface on its own.
- *Score against the eval smoke test / offline goldens as the source of truth.* Rejected as the
  primary source: the live eval meter is 5 fixed queries (none Japanese, none niche) — a toy
  thermometer that cannot see a real fever. Eval is demoted to a **calibration baseline**;
  production traffic is the source.
- *Put the analysis (grouping, trend, clustering) inside the Overseer bucket.* Rejected: it would
  duplicate domain logic outside the domain and force raw data to be streamed out, breaking the
  bucket's "reads the verdict, does not re-run the pipeline" rule. Analysis lives in the go-api
  enabler, next to the data.
- *Auto-cluster failing cases and have the platform name the emergent group itself.* Rejected for
  now: it is ML-ish, can mislead, and is a real build. Present rich cases + group-on-demand gets
  ~90% of the value honestly. Parked as a future signal, not a v1 dependency.

## Assumptions, the unstated beliefs it rests on

- **[load-bearing]** The real search/detail path already fans out to multiple providers, so
  cross-provider disagreement is capturable passively during real requests without extra provider
  calls. (Confirmed in the map: `fanout.go` records per-provider `Results`/`Status`/`ResultCount`.)
- **[load-bearing]** go-api can record and serve new per-case quality signal without the Overseer
  reaching into its internals — the enabler exposes operator endpoints, the bucket reads them.
- **[load-bearing]** Cross-provider disagreement is a good-enough proxy for "suspect" quality.
  A universal, no-ground-truth signal will have false positives; the platform treats it as a hint
  to judge, so false positives cost owner attention, not correctness.
- Production traffic is real user data; storing queries + behavior needs a retention/anonymization
  discipline (below), even though only the owner ever views it.
- Passive per-case recording is cheap enough to run on every real request; only the eval baseline
  needs scheduling.

## Scope / non-goals, what's in and explicitly what's OUT

**In scope:**

- The deepened bucket's **spine**: the universal engine (rich per-case capture + surface-worst +
  group-on-demand), cross-provider disagreement as the primary "suspect" signal, production as the
  source, eval as calibration.
- All **seven categories** as named slots on that spine.
- The **go-api domain-quality enabler** that records and serves the production quality signal.
- The **first slice** built end to end: the enabler for Discography + the bucket render for it
  (see Priority), proving the cross-provider engine on real requests.
- **Catalog / detail correctness** is now IN (it was OUT in the shipped bucket) — as the
  Discography category.

**Out of scope / non-goals:**

- **Acting on the app.** The bucket observes and hints only. No re-run, no re-acquire, no auto-fix.
  (Platform-wide observe-only invariant.)
- **Fixing any of the surfaced quality problems.** Surfacing + hinting is the whole job; fixes are
  separate work the owner acts on.
- **Auto-clustering / emergent group *naming*.** Deferred (see rejected alternatives). The
  platform surfaces and lets the owner group; it does not name the cluster itself yet.
- **True language detection and an up-and-coming / rising-artist signal.** Not recorded anywhere
  today; the existing script-class and popularity-band proxies stand in until a real need is
  proven. Parked.
- **Deep per-category signal specs for all seven at once.** Only the first slice is specified
  deeply; each category grows its signals in its own later pass. "Dig deeper on each step" is
  expected and healthy, not a v1 requirement.
- **Replacing the offline eval harness.** It stays as the calibrator and the golden corpus.

## Priority, must-have vs nice-to-have

**Must-have (the first slice):**

- The go-api enabler surface for **one** category, recording cross-provider per-case signal on real
  requests and serving worst-case lists + aggregates operator-only.
- **Discography / detail correctness** rendered end to end in the bucket: real artists whose page
  shows a contamination suspect or an incompleteness gap, surfaced worst-first, each case carrying
  the provider-by-provider evidence to act. Chosen first because it **exercises the cross-provider
  engine for real**, proving the spine early, and it is the owner's motivating example.
- Group-on-demand by at least one recorded field, to prove the "let the pattern show itself" move.

**Then (each its own slice, rough order):**

- **Coverage** — mostly needs existing zero-result / `CoverageReportA` data *exposed* (cheap
  fast-follow), and it is where languages/niche/up-and-coming emerge.
- **Search ranking**, then **Query understanding**, **Identity / matching**, **Acquisition + audio
  quality**, **Provider health as a quality driver**.
- Deeper signals within any category as the owner learns which cases hurt most.

**Nice-to-have (later):** auto-clustering; true-language + rising-artist dimensions;
user-behavior as a second truth source.

## Invariants, the bucket's core rules (living; grows as the build reveals more)

Inherits the platform + bucket #6 spine and adds production-data rules. Each stated so a test could
check it.

- **Observe-only / no-act** — no path mutates the watched app or re-runs a pipeline; the enabler
  endpoints are reads, the bucket client exposes no mutating method. (Allowlist reflection guard,
  as the shipped bucket already has.)
- **Disagreement is a hint, never a verdict** — a surfaced case is labelled "suspect" with its
  evidence, never "wrong"; no code branch treats a disagreement signal as ground truth. (A test
  asserts suspect cases render as hints with provider evidence, not as assertions.)
- **Bounded storage, always** — every production sample store is a ring or windowed rollup with a
  fixed cap; no per-case capture can grow storage without limit. (Feed N×capacity, assert size
  stays capped.)
- **Production data is windowed + anonymized** — real user queries/behavior are retained only
  within a bounded window and the user identity is stored hashed, never raw. (A test asserts no raw
  user id is persisted and samples past the window are evicted.)
- **Owner-only** — no unauthenticated or non-owner request reaches Domain quality data (bucket
  render behind the owner guard; enabler endpoints operator-only).
- **Independent degrade, don't crash** — each category degrades on its own; a source down flags
  only its slice STALE with last-known value; the bucket never takes down the shell. (Inherited;
  extended per new source.)
- **Render-escaping** — every watched-app field (real query strings, titles, artist names) is
  HTML-escaped before render. (Inherited; critical since production query text is now shown.)
- **Additive** — the deepening adds its own files + one registration point in the Overseer and its
  own enabler files in go-api; it does not edit other buckets or the go-api read client's core.

## Open questions, the parked unknowns

None of these block designing the spine or the first (Discography) slice. They belong to later
category passes or are tunable at build.

- **Disagreement thresholds** — how much cross-provider disagreement counts as a "suspect"
  (e.g. album-count gap %, single-provider-only track). Tunable; settled at build against real data.
- **User-behavior as a second truth source** — when/how to layer click/skip/abandon signal onto
  the cross-provider engine (weighting, noise handling, minimum traffic). Future slice.
- **True-language detection + an up-and-coming / rising-artist signal** — worth recording only once
  a real need is proven; proxies stand in until then. Deferred by choice.
- **Auto-clustering / emergent group naming** — the fully-automatic "the platform names the weak
  cohort itself" step. Deferred by choice.
- **Exact retention window length** — the bounded-window value for production samples (a knob;
  default to a few weeks, enough to see multi-week drift). Settled at build.
- **Per-category deep signal specs (categories 2–7)** — each earns its own deeper pass; named here,
  not fully specified here.

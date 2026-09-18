# Domain quality — deeper (what the bucket shows, and the engine under it)

Expansion brief on top of `domain-quality-deep.md` (the 7-category deepening, already shaped and
designed). This one does **not** restate the seven categories or the go-api enabler decision — read
the deep brief for those. It settles two things the deep brief left fuzzy:

1. **What the bucket actually shows and what the owner derives from it** (it read as abstract).
2. **The suspect engine** — how "this looks wrong" is decided, now that Genius-as-truth and
   hand-set trust weights were weighed and dropped in favour of an id-anchored signal.

Fed by the lexicon's *Data and Analytics → Data Quality and Semantics* chapter (the "data can be
present and still be wrong" frame), *Consumer-Side Observability* (show freshness), and *Pipeline
Health and Freshness* (green ≠ fresh).

## Vision, what it's for: the problem, the user, the outcome (the goal + why)

The owner cannot see, at a glance, whether the core journey (type a query → get results → open a
page → play/acquire) is actually **good right now**, and when it is not, cannot see **where** it
breaks without hand-digging logs and the CLI eval harness.

The honest framing that drove this whole session: **the bucket can never stamp "the data is
right."** No ground truth exists for a music catalog stitched from many providers. So every view
answers a softer, truthful question — *"where does our catalog look wrong, and how sure are we?"*
It shows **suspects and confidence, never a clean yes.** That matches the lexicon's central point:
the dangerous failures are the ones where data is present and plausible but wrong (a query "fix"
that looks like a hit, a track attributed to the wrong artist, a file that plays but is the wrong
recording). Those pass ordinary monitoring untouched, so a dedicated quality lens is the only thing
that catches them.

Outcome wanted: open Domain quality and it turns "search feels a bit off lately" into a ranked,
evidenced worklist of *what to fix* — the weak class, whether it is getting worse, which step owns
it, and enough per-case evidence to aim the next code change instead of guessing.

## The idea, the proposal itself, the reasoning, and the alternatives weighed and rejected

### What the bucket shows (the ladder, glance → detail)

Top to bottom the panel reads as the user's journey; each step answers the same three questions —
**how bad, is it getting worse, what class to fix.**

- **Headline** — is the product good right now: live eval score, acquisition success rate, and a new
  **suspect rate** (share of real requests that tripped a suspect signal). The "should I worry"
  glance.
- **Worst real cases now** — actual artists/tracks, worst-first, each carrying its evidence (which
  sources disagreed and on what, or which file check failed). This is the "why" — you click into a
  real contaminated page and see the provider-by-provider breakdown.
- **Group-on-demand** — group the current cases by any recorded field so the pattern pops (e.g. 60%
  of this week's failures are Japanese-script artists). This is the derive step: it turns a pile of
  cases into one fixable class, and it is how languages / niche / rising artists surface **without
  anyone naming them up front.**
- **Per-step health** — the seven categories, each with its own state, so you see the break is in
  matching, not search.
- **Drift** — each measure tracked over a window, flagged when it moves. The slow-rot catcher, and
  the only thing that catches a source silently changing a mapping upstream (a snapshot misses it).
- **Freshness per slice** — the age of each category's last real sample, shown, so a dead feed can
  never quietly mislead the owner (consumer-side trust: never present a stale number as live).

### The suspect engine: two families of "wrong", two instruments

The clarifying insight of this session: **domain quality is not one engine.** There are two kinds of
"wrong" and they need different signals.

- **Discovery correctness** (query understanding → search ranking → coverage → identity/matching →
  discography). **No ground truth exists**, so the suspect signal is **id-anchored cross-source
  disagreement**. Results already carry MBIDs (confirmed: `service/identity_stamp.go`, a durable
  identity store, an MBID index, external-id xref bridging). A shared ISRC/MBID is a **hard fact,
  not a vote**: sources stamping the same id are provably the same recording. So the engine asks
  "does this attribution line up with the ids the others carry?" A source claiming a track for an
  artist with a mismatched or absent id, while the rest agree on one id, is the suspect. **Plain
  headcount is only the fallback** for the results that carry no id (the code has an explicit "no
  MBID → skip" path, so id coverage is real but partial — the fallback earns its place).

- **Acquisition correctness** (acquisition + audio quality). Here there **is** a checkable truth: the
  file either is real audio that matches the requested track, or it is not. No disagreement needed —
  the pipeline already runs the checks (ffprobe, fpcalc, fingerprint match). The suspect signal is a
  per-stage rejection breakdown plus the acquisition silent-killer: we got *a* file, it plays (green
  on "did we get something"), but the fingerprint says it is the wrong recording, a preview clip, or
  a live cut. Those surface worst-first.

- **Provider health** sits under both — a source degrading quietly poisons discovery *and*
  acquisition at once. The question is "which source is dragging quality down", not "which is slow"
  (that is backend-perf).

Trust is **not hand-set** and **no source is crowned truth.** The id does the heavy lifting; for the
id-less cases, source reputation may **emerge** over time from each source's own track record, as a
tiebreaker only. Every discovery signal is a **hint, never a verdict** — a case renders as "suspect"
with its evidence, never "wrong".

### A light sanity-check layer (what became of the "declared invariants")

A tiny set of universal checks rides alongside the engine — a track should carry at least one id, a
known artist should not have zero albums, a query "fix" should not jump script class. These are the
one thing structural disagreement **cannot** catch: the case where every source agrees and every
source is wrong. Kept deliberately small — a handful of sanity checks, **not** a rules engine, and
mostly already expressed as the id-presence check.

### What the owner derives (the payoff)

From the tracked data: **the weak class** (group → the pattern; the "improve the algorithm" signal,
it points at the one change that hardens a whole class); **whether it is getting worse** (drift);
**which step owns it** (fix the right code); **which provider drags quality down** (reputation); and
**what to fix first** (cases rank by how many real users a class hurts).

### What more we do with it: the eval-corpus loop

The one place the bucket reaches back. Today the eval harness is 5 fixed toy queries; this bucket
finds real edge cases in production daily. The owner can **promote a surfaced failure into the golden
eval corpus** so it becomes a permanent regression test. Because there is no ground truth, promoting
means the **owner eyeballs the case and declares the correct answer**, and *that* becomes the test.
Production becomes a discovery engine for the regression suite. Stays observe-only — it exports a
case, it never touches the app.

### Alternatives weighed and rejected (this session)

- **Genius as the source of truth for discographies.** Rejected. Genius is lyrics-first; it indexes
  songs with lyrics/annotations, not full release catalogs, so discography coverage is patchy.
  Discogs and MusicBrainz are already in the provider set and are purpose-built release databases —
  stronger for this exact job. Deeper reason: **no single source should be crowned truth** (the
  "present but wrong" problem just moves onto Genius), and Genius is not in the existing fan-out, so
  checking it costs a new outbound call per case and breaks the deep brief's passive-capture bet.
- **Hand-set trust weights per source.** Rejected. Annoying to tune, brittle, and it bakes in
  today's guesses.
- **Fully self-learned trust from agreement alone.** Rejected as the primary mechanism. With no
  anchor it learns "who sides with the pack", but the pack can be wrong: cheap sources copying the
  same bad data punish the one good source for being the outlier — it inverts exactly when it
  matters. Ids anchor it instead; reputation is a tiebreaker only.
- (Carried from the deep brief, not re-argued here: pre-named cohorts, eval-as-truth, analysis in the
  Overseer, auto-clustering.)

## Assumptions, the unstated beliefs it rests on; mark the load-bearing ones

- **[load-bearing]** Results carry MBIDs on enough real requests for the id-anchor to be the primary
  signal. **Confirmed in code** (`identity_stamp.go`, durable identity store, MBID index, xref
  bridging), with partial coverage — hence the headcount fallback. Exact coverage % is an open
  question below.
- **[load-bearing]** Acquisition already records per-stage reason codes and the fingerprint-match
  result per attempt, so acquisition suspects need no new checks, only serving. (Deep brief's map;
  confirm exact fields in design.)
- **[load-bearing]** Discogs and MusicBrainz are in the discovery fan-out, so their id-backed sets
  are available passively for the discography comparison without extra calls.
- Drift needs the enabler to keep a bounded windowed rollup per measure, not just point-in-time
  samples.

## Scope / non-goals, what's in, and explicitly what's OUT

**In:**
- The **show-ladder** made explicit (headline incl. suspect rate → worst cases → group-on-demand →
  per-step → drift → per-slice freshness).
- The **two-family suspect engine**: id-anchored disagreement for discovery, file/fingerprint checks
  for acquisition, provider health under both.
- The **light sanity-check layer** (small, id-shaped).
- **Drift** as a first-class signal, and **per-slice freshness display**.

**Out / non-goals:**
- **Genius (or any single source) as ground truth.** Rejected above.
- **Hand-set trust weights.** Rejected above.
- **The eval-corpus promote loop as first-slice work.** Named and wanted, but it is a fast-follow
  (needs the owner-declares-answer flow and a corpus format — see open questions).
- **Emergent source reputation as a v1 dependency.** The id-anchor ships first; reputation is a later
  tiebreaker for id-less cases.
- Everything the deep brief already put out of scope (acting on the app, fixing the problems,
  auto-clustering / emergent group naming, true-language + rising-artist dimensions, replacing the
  offline eval harness). Unchanged.

## Priority, must-have vs nice-to-have, separated

**Must-have (first slice, unchanged target: Discography):**
- The id-anchored disagreement engine proven on **Discography** end to end — real artists whose page
  shows a contamination suspect (a track the ids don't back) or an incompleteness gap (albums the
  id-backed sources list and we don't), surfaced worst-first with provider-by-provider evidence.
- The headline suspect rate and the worst-cases list rendered for that slice.
- Group-on-demand by at least one recorded field.
- Per-slice freshness shown.

**Then (each its own slice):**
- **Acquisition** slice (file/fingerprint engine + per-stage breakdown) — different instrument, high
  value, and its checks already exist.
- **Drift** lines across the shipped slices.
- The remaining discovery categories (coverage, search ranking, query understanding, identity).
- **Provider health as a quality driver.**

**Nice-to-have (later):** the eval-corpus promote loop; emergent source reputation; user-behavior as
a second truth source.

## Must-holds, the feature's core rules (living; grows as the build reveals more)

Inherits every rule from `domain-quality-deep.md` (observe-only, disagreement-is-a-hint, bounded
storage, windowed + anonymized production data, owner-only, independent degrade, render-escaping,
additive). Adds:

- **Id is the anchor, headcount is the fallback** — a discovery suspect judged against shared
  ISRC/MBID is decided by id agreement; plain source headcount is used only when no id is present.
  (A test asserts an id-mismatch case is flagged even when the majority of sources agree, and that
  headcount is not consulted when an id decides it.)
- **No source is crowned truth** — no code path treats any single provider (Genius included) as
  ground truth. (A test asserts no provider short-circuits the engine to "correct".)
- **No hand-set trust weights** — trust is never a static per-source constant in config or code; it
  is either the id fact or emergent reputation. (A test/lint asserts no static weight table.)
- **Acquisition verifies, never guesses** — an acquisition suspect is decided by a real file check
  (ffprobe/fpcalc/fingerprint), never by disagreement. (A test asserts a fingerprint-mismatch case
  is flagged even at a "success" HTTP outcome.)
- **Freshness is shown, never faked** — every rendered slice carries the age of its last real
  sample; a stale slice renders as stale, not as a live value. (A test asserts a past-window slice
  renders STALE with last-known age.)
- **Suspect rate counts real requests only** — the headline suspect rate is computed over production
  traffic within the bounded window, not over eval or synthetic requests. (A test asserts eval runs
  do not move the suspect rate.)

## Open questions, the parked unknowns, for the readiness gate

Each below carries a default so `design` can proceed on the first (Discography) slice; none blocks it.

- **MBID/ISRC coverage %** — what fraction of real discography results actually carry a shared id.
  *Default:* id-anchor primary, headcount fallback for the rest; measure real coverage during the
  first slice and revisit if it is low. Not a blocker.
- **Disagreement thresholds for the id-less fallback** — how big an album-count gap, or a
  single-source-only track, counts as a suspect when no id decides it. *Default:* tune at build
  against real data (inherited from the deep brief).
- **Drift window and "meaningful move"** — the window length and the size of a move worth flagging,
  per measure. *Default:* a few weeks, flag on a clear step-change; tunable at build.
- **Emergent source reputation** — how to compute it honestly for id-less cases without the
  wrong-majority trap (cold start, weighting). *Later slice, not first-slice.*
- **Eval-corpus promote flow** — the owner-declares-the-answer UX and where the golden corpus lives
  / its format. *Fast-follow; design when that slice is picked up.*
- **Exact suspect-rate formula** — per-category vs one global number, and the precise numerator.
  *Default:* one global headline number plus a per-category rate; settle at build.

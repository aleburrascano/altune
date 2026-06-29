---
date: 2026-06-29
session-context: discovery ranking quality — artist-intent burial
tags: [ranking, discovery, eval, measurement]
related-vault: ["wiki/concepts/Ubiquitous Language.md"]
---

# Kind-blind ranking ties are decided by a coverage proxy, not intent — measure before you fix

## The pattern

When a ranking measure ties across results of *different kinds* (artist vs track vs
album), the tie falls through to a tiebreak that proxies **provider coverage**
(multi-source count, RRF) rather than **what the user meant**. On a bare-token query
the lone token exact-matches every candidate's title, so relevance ties at 1.0 and
the kind ordering becomes arbitrary: a same-name track usually out-covers the artist
card and buries it (you typed `vax`, the artist sits at #4 under three "Vax" tracks),
while occasionally an over-covered obscure artist buries the famous song (you typed
`firework`, an unknown "FireWork" artist outranks Katy Perry's track). Same shape,
opposite victims — proof there is no *query-side* kind rule.

## When it bites

- A bare single-token query that is simultaneously an artist name and a common
  track/album title (`boston`, `genesis`, `rush`, `vax`, `firework`).
- Any time relevance saturates (everything matches equally) and a downstream
  tiebreak that was never meant to express intent silently decides the answer.
- It is invisible to an eval whose oracle only passes on one kind — the discovery
  library eval hard-requires `ResultKindTrack`, so artist-intent burial never
  tripped a gate even though it nagged in real use.

## What to do

- **Build the missing measurement first.** A kind-specific eval corpus
  (`discoveryeval -mode artist-intent -corpus hard`: bare artist name → expect the
  artist card top-K) turns "feels wrong" into a number. Split the failure into
  **buried** (artist present, out-ranked — ranker-fixable) vs **absent** (never
  surfaced — a recall/identity gap no reorder can fix). The split stops a "boost
  artists" change from looking like it worked when the real failure was recall.
- **Run the eval at concurrency 1.** The bug needs all providers present; under
  concurrency iTunes rate-limits out, the same-name tracks vanish, and the burial
  hides (a false "100% clean"). A/B on **recorded fixtures**, not live — the live
  oracle's iTunes-flake noise (±3.5pp margin) swamps a real ranking delta.
- **Fix the tie with a cross-kind, kind-gated prominence rung.** Among tied results
  of *different kinds*, order by `log1p` of the provider prominence already in the
  data (Deezer `nb_fan` for artist/album, `rank` for track). Parameter-free; fires
  *only* across kinds so track-vs-track order is byte-identical (the bare-title
  corpus the popularity attempt regressed cannot move).
- **Gate it behind a flag and A/B all corpora on the same fixtures.** Ship only if
  it improves the target without regressing the others past their noise margins.

## Why this is true

Relevance is a similarity measure; it deliberately cannot tell artist from track
when both exact-match the query. The tiebreak ladder beneath it
(`behavioral → popularity → multi-source → RRF`) was built to break ties *within*
a result set, and multi-source/RRF measure how many providers returned an entity —
a proxy for catalog breadth, **not** for which kind the user intended. Tracks
structurally out-cover their matching artist card (every cover/version/upload is a
track row across providers; the artist is often one card from one provider), so the
artist systematically loses. Prominence is the only signal that separates the two
directions correctly — a famous artist *and* a famous track both win their
comparison — because it measures magnitude within each kind, log-compressed so the
differing raw scales (track `rank` ≤ 1M, artist `nb_fan` unbounded) are comparable.

Measured A/B (recorded fixtures, only the ranker differs), prominence OFF → ON:

| Corpus | Metric | OFF | ON |
| --- | --- | --- | --- |
| track-exact (product bar) | top-3 | 99.6% | 99.6% (identical) |
| track-hard (bare title) | top-3 | 77.0% | **80.4%** |
| track-hard | artist wrongly #1 | 47 | **9** |
| artist-intent (bare name) | top-1 | 78.9% | **86.7%** |
| artist-intent | top-3 | 94.5% | 95.3% |

The `47 → 9` collapse is the `firework` failure fixed and generalized across 38
queries; the artist-intent top-1 jump is the `boston`/`journey`/`genesis` burials
cleared.

## What this does NOT fix

The cross-kind rung cannot rescue **low-prominence** cases, and that residue is a
**personalization** problem, not a ranking one:

- `witchcraft` → your owned Lucki track loses to Pendulum's — same-kind
  (track-vs-track), which the rung never touches, and globally Pendulum wins. Only
  a library-aware boost surfaces it.
- the specific underground "Vax" (the `Blue Dawn` artist) — low `nb_fan`, plus a
  same-name identity ambiguity (the Icelandic "Vax" is the one that surfaces).

`buried_rate` stayed at 4.7% precisely because the remaining buried artists are
low-fan. The library is the ground truth your eval already rewards, so the next
lever is a **library-aware / behavioral boost** on ambiguous bare queries.

## Anti-pattern to avoid

- **Special-casing the example queries.** `vax`/`firework`/`witchcraft` pull in
  opposite directions; any per-query fix or hardcoded bank rots immediately.
- **A blanket popularity signal.** Applied *within* a kind (track-vs-track) it
  regresses a niche library — the famous track buries the owned one (measured
  81%→75% in the 2026-06-24 reversion). Cross-kind-only is what dodges that.
- **Trusting a high-concurrency eval.** Provider dropout silently removes the bug
  from the sample. Faithful measurement is concurrency-1 + recorded fixtures.

## See also

- `services/go-api/internal/discovery/service/rank.go` — `rankWithProminence`, the rung
- `services/go-api/internal/discovery/service/eval/artist_intent_eval.go` — the eval corpus
- `services/go-api/internal/discovery/CLAUDE.md` — ranking key + eval discipline
- docs/adr/0007 (ranking overhaul) — the rebuild this extends

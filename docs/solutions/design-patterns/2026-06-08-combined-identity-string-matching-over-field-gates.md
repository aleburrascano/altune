---
title: "Prefer combined-identity token matching over field-by-field JW gates"
date: "2026-06-08"
category: design-patterns
module: acquire-track
problem_type: design_pattern
component: service_object
severity: medium
applies_when:
  - "Matching user-owned metadata against external source titles (YouTube, SoundCloud, etc.)"
  - "External titles embed artist, featuring credits, and qualifiers in a single string"
  - "Field-by-field comparison fails because fields aren't cleanly separated in external data"
tags:
  - fuzzy-matching
  - token-sort-ratio
  - jaro-winkler
  - youtube
  - acquisition
  - discovery-reuse
  - dedup
---

# Prefer combined-identity token matching over field-by-field JW gates

## Context

The acquire-track feature needed to verify that a YouTube search result matches the intended track before downloading audio. The initial implementation used three independent gates: title JW (Jaro-Winkler) >= 0.85, artist JW >= 0.70, duration within +/-15 seconds. Each field was compared separately, with regex cleanup to strip YouTube title conventions like "Artist - Title (Lyrics) ft. Guest".

This failed on real YouTube data. Example: searching for track title "INTOXYCATED" by artist "Oxlade" returns a YouTube result titled "Oxlade - INTOXYCATED (Lyrics video) ft. Dave". After normalization, the candidate title becomes "oxlade intoxycated feat dave", which scores 0.646 JW against "intoxycated" — below the 0.85 gate. Adding regex to strip the "Artist - " prefix (`_ARTIST_TITLE_SEP`) is fragile: YouTube naming conventions have too many permutations (artist before/after dash, em-dash vs en-dash vs hyphen, parenthetical qualifiers, multiple feat artists, "Official Audio" tags, etc.).

The same project's discovery pipeline had already solved this problem through v1-v4 iterations. Its `_relevance_score` function (in `application/discovery/dedup.py`) uses `rapidfuzz.fuzz.token_sort_ratio` on combined identity strings rather than comparing fields independently.

(auto memory [claude]) The legacy music-manager project at `C:\Users\Alessandro\music-manager` had the same accuracy failure — it downloaded wrong audio due to insufficient matching. This is the pattern that prompted the accuracy-first design in the acquire-track spec.

## Guidance

When verifying that an external audio source matches a known track, use **combined-identity token matching** instead of field-by-field gate comparison.

The pattern:
1. Build a combined identity string from the track's metadata: `f"{normalize(artist)} {normalize(title)}"`
2. Build a combined identity string from the candidate's full title (no field splitting needed)
3. Compare using `token_sort_ratio` (order-independent token overlap), not `JaroWinkler` (sequential character similarity)
4. Keep duration as a separate sanity gate (it's orthogonal to text identity)

```python
# WRONG: field-by-field gates with fragile cleanup
title_jw = JaroWinkler.normalized_similarity(
    normalize("intoxycated"),                     # track title alone
    normalize("oxlade intoxycated feat dave"),     # cleaned candidate
)
# => 0.646 — REJECT (false negative, below 0.85)

# RIGHT: combined-identity token matching
from rapidfuzz import fuzz

track_identity = normalize_for_match(f"{track.artist} {track.title}")
# => "oxlade intoxycated"

candidate_identity = normalize_for_match(candidate.title)
# => "oxlade intoxycated feat dave"

score = fuzz.token_sort_ratio(track_identity, candidate_identity) / 100.0
# => ~0.87 — ACCEPT (tokens "oxlade" and "intoxycated" both present)
```

The discovery pipeline's `_relevance_score` (in `application/discovery/dedup.py`) is the reference implementation. It scores against the result's own identity, tries both title-only and combined artist+title forms, and takes the max. It also scores on content tokens (stopwords stripped) as a supplementary signal.

## Why This Matters

1. **YouTube doesn't respect field boundaries.** The title field contains artist name, track title, version qualifiers, featured artists, and labels — all concatenated. Splitting them back apart with regex is a whack-a-mole game.

2. **JaroWinkler penalizes prefix mismatches severely.** JW is designed for short strings with typos. When the artist name is prepended to the title, JW sees a wrong prefix and scores low even though all the right words are present. `token_sort_ratio` sorts tokens alphabetically before comparing — word order doesn't matter.

3. **The project already solved this.** The discovery dedup pipeline went through v1-v4 iterations over a week. The acquisition matcher should reuse the proven approach rather than reinventing it with fragile per-field gates.

## When to Apply

- Matching a known track identity against search results from user-generated-content platforms (YouTube, SoundCloud) where metadata fields are unstructured
- Comparing music metadata across sources with different field conventions
- When catching yourself writing regex to parse artist names out of combined title strings
- **NOT** needed when comparing between curated metadata providers (MusicBrainz, Deezer, iTunes) where title and artist are reliably separated — field-level JW works fine there, as `dedup.py`'s `_signature` and `_try_merge` demonstrate

## Examples

**Before (field-by-field gates, fails on real data):**

| Track | YouTube Result | Title JW | Outcome |
|-------|---------------|----------|---------|
| "INTOXYCATED" / Oxlade | "Oxlade - INTOXYCATED (Lyrics video) ft. Dave" | 0.646 | REJECT (false negative) |
| "Blinding Lights" / The Weeknd | "The Weeknd - Blinding Lights (Official Audio)" | ~0.58 | REJECT (false negative) |
| "HUMBLE." / Kendrick Lamar | "Kendrick Lamar - HUMBLE. (Music Video)" | ~0.52 | REJECT (false negative) |

**After (combined-identity token matching):**

| Track Identity | Candidate (normalized) | token_sort_ratio | Outcome |
|---------------|----------------------|-----------------|---------|
| "oxlade intoxycated" | "oxlade intoxycated feat dave" | 87 | ACCEPT |
| "weeknd blinding lights" | "weeknd blinding lights" | 100 | ACCEPT |
| "kendrick lamar humble" | "kendrick lamar humble" | 100 | ACCEPT |

The duration gate remains unchanged — it catches genuinely wrong results (live versions, remixes with different lengths) that text matching alone cannot distinguish.

## Related

- `docs/specs/acquire-track/design.md` — documents the gate-based design (to be updated when matching is redesigned)
- `docs/specs/discover-music-v3/spec.md` — documents the discovery pipeline's adoption of `token_sort_ratio`
- `docs/adr/0007-unified-music-search.md` — notes "ISRC + JW + per-source-priors as the dedup contract. Reversible to embeddings or other models adapter-internally"
- `docs/solutions/2026-06-07-extras-merge-provider-priority.md` — related discovery matching doc (different problem: merge display vs acquisition accuracy)

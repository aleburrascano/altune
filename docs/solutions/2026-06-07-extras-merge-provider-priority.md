---
category: pattern
area: discovery
tags: [dedup, merge, multi-provider, ranking]
---

# Extras merge needs provider-priority awareness for display fields

## Problem

When `fuse_and_rank` merges search results from multiple providers, `_merge()` combines extras dicts with "canonical (highest prior) wins." This works for identity fields (ISRC, MBID) but produces wrong values for **display fields** like `album` — MusicBrainz (prior 0.95) returns the first-release title (often a compilation), overwriting Deezer's (0.85) accurate commercial album name. Worse: multiple MB recordings of the same song (from different releases) all merge sequentially, each overwriting the album name. The final value is from whichever MB recording merged last — effectively random.

## Solution

**Pre-merge lookup + post-merge stabilization.** Before the merge loop, scan all raw provider results and build a `_album_best: dict[str, tuple[str, float]]` mapping `(signature → album_name, provider_prior)`, keeping the lowest-prior provider's value (Deezer/iTunes = most user-facing). After the merge loop, re-stamp each accumulated result's `extras["album"]` from this lookup. This decouples the album name from merge order entirely.

The general pattern: pairwise merge is fine for **identity** fields (ISRC, MBID, sources) but toxic for **display** fields where "canonical wins" picks the wrong source. Post-merge stabilization from a pre-computed lookup is the cleanest fix.

## Also learned

When changing a frontend request parameter (e.g., `limit=30` → `limit=100`), always check the backend's validation constraints (`Query(..., le=50)` in FastAPI). A validation mismatch produces a silent 422 that makes the entire response appear empty — no error shown to the user, just missing data.

# Acquire Track — Source Selection Redesign (v2)

> Brainstormed 2026-06-09. Supersedes the keyword-based matching in design.md.
> Previous approach: keyword banks (`_AUDIO_KEYWORDS`, `_VIDEO_KEYWORDS`, etc.) + `_version_preference()`.
> Problem: every edge case (music videos, censored versions, covers) adds another keyword. Doesn't scale.

## Core change

Replace keyword-based filtering with a **three-tier source selection** that uses structured metadata from YouTube instead of parsing natural language titles.

`matching.py` becomes a **ranker** (scores candidates by structured quality signals) instead of a **filter** (checks keyword presence). No keyword bank to maintain.

## Three-tier source selection

### Tier 1: ISRC deterministic lookup

When the track has an ISRC (from Deezer/MusicBrainz via discovery enrichment):
- Search YouTube Music directly: `https://music.youtube.com/search?q={isrc}`
- If a result matches the ISRC, it's the exact recording — zero ambiguity
- Skip all scoring/ranking — this is deterministic

### Tier 2: Structured metadata ranking

When ISRC isn't available or Tier 1 finds nothing:
- Search YouTube with `{title} {artist}` (same as current)
- For each candidate, extract structured metadata via yt-dlp (not just title/artist/duration):
  - `channel`: channel name (detect "- Topic" suffix, "VEVO" suffix, verified badge)
  - `categories`: YouTube's own categorization (e.g., `["Music"]`)
  - `view_count`: engagement signal (official versions dominate views)
  - `duration`: exact match vs approximate
  - `channel_follower_count`: helps distinguish official channels from reuploads
- Rank candidates by a composite of these signals — no keywords, no title parsing
- Pick the highest-ranked candidate that also passes identity matching (`token_sort_ratio >= 70`)

### Tier 3: Best-effort identity match

When Tier 2 finds no candidate with strong metadata signals:
- Same search results, just `token_sort_ratio` + duration gate (±15s)
- Picks the best identity match regardless of metadata quality
- Logs with `confidence: "low"` so accuracy can be monitored
- This tier handles unreleased/indie tracks that have no Topic channels or verified metadata

### Failure

All tiers exhausted → `FAILED` with reason

## Structured metadata ranking (Tier 2 detail)

Candidates are scored by a composite of structured signals. Each signal produces a score 0.0-1.0:

| Signal | Score 1.0 | Score 0.5 | Score 0.0 |
|--------|-----------|-----------|-----------|
| Channel type | "- Topic" (auto-generated official) | "VEVO" or verified channel | Unverified / fan channel |
| Category | `"Music"` | Other | Missing |
| Duration match | Within ±2 seconds of expected | Within ±15 seconds | Beyond ±15 seconds or unknown |
| View count | Top quartile of candidates | Middle | Bottom quartile |

Composite: weighted sum. Channel type and duration are the strongest signals (they directly indicate "official audio" and "right recording").

No hardcoded keywords. No title parsing. YouTube controls these metadata fields.

## What changes in matching.py

### Removes
- `_AUDIO_KEYWORDS`, `_VIDEO_KEYWORDS`, `_CLEAN_KEYWORDS`, `_EXPLICIT_KEYWORDS`
- `_version_preference()` function
- `_IDENTITY_THRESHOLD` as a single magic number (each tier has its own logic)
- Any regex for title cleaning

### Keeps
- `normalize_for_match` import (normalization is not heuristic — it's consistent text preprocessing)
- `token_sort_ratio` for identity scoring (proven approach from discovery)
- `duration_gate` concept (but tighter in tier 2: ±2s; relaxed in tier 3: ±15s)
- `identity_score()` function

### Adds
- `AudioCandidate` gains: `channel`, `categories`, `view_count`, `channel_follower_count`
- `rank_candidates(track, candidates) -> list[RankedCandidate]` — structured metadata ranker
- ISRC lookup path in SearchStep

## What changes in AudioCandidate

```python
@dataclass(frozen=True, slots=True)
class AudioCandidate:
    title: str
    artist: str
    duration_seconds: int | None
    url: str
    # New structured metadata fields
    channel: str = ""
    categories: tuple[str, ...] = ()
    view_count: int = 0
    channel_follower_count: int = 0
```

## What changes in YtDlpAudioSearcher

The adapter already extracts `title`, `artist`, `duration`, `url` from yt-dlp results. It needs to also extract: `channel`, `categories`, `view_count`, `channel_follower_count`. These are standard yt-dlp info_dict fields — no additional API calls needed.

For Tier 1 (ISRC lookup), add a new search path that queries YouTube Music directly.

## What changes in SearchStep

The search step needs to:
1. Try ISRC lookup first (if ISRC available)
2. Then run the standard `{title} {artist}` search
3. Pass all candidates (with metadata) to the ranker

The tiered logic moves from the step into the ranker — the step just collects candidates, the ranker decides.

## Testing

- Unit tests for the ranker with canned metadata (no yt-dlp dependency)
- Topic channel detection: channel name ending in "- Topic"
- Category-based ranking: Music > other
- Duration precision: ±2s vs ±15s tiers
- Integration test: known ISRC → exact match

## Dependencies

- No new external dependencies
- yt-dlp already returns all the metadata fields we need
- `normalize_for_match` and `token_sort_ratio` already in use

## Related

- `docs/solutions/design-patterns/2026-06-08-combined-identity-string-matching-over-field-gates.md` — the lesson that led to `token_sort_ratio`. Still applies; this redesign builds on top of it.
- `docs/specs/acquire-track/design.md` — original gate-based design (superseded by this for source selection)

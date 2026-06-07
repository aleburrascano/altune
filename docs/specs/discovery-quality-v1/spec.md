# Discovery Quality v1

> Spec for `discovery-quality-v1` — version 1, drafted 2026-06-07.
> Authors: solo + Claude.
> Status: Draft.

## Problem

After shipping discovery v4 with multi-provider artist content (Deezer, MusicBrainz, Last.fm, SoundCloud), several data quality issues surfaced across the discovery pipeline:

1. **SoundCloud sets are broken** — all sets hardcoded as `record_type="ep"`, tapping a set shows "Couldn't load tracks" (no `AlbumContentProvider` implementation), and set listing has no artwork.
2. **Search ranking doesn't surface originals first** — searching "super shy" doesn't reliably show NewJeans as the top result; unmarked covers/remixes tie in the same relevance band and can outscore originals on popularity.
3. **Album names are inconsistent between sessions** — the merge in `_merge()` lets `None` from a higher-prior provider overwrite a real album name from a lower-prior one, and provider response timing can affect which extras win.
4. **MusicBrainz type=None entries pollute discography** — release-groups without `primary-type` default to the Albums bucket, adding noise (e.g., "game over (with Destroy Lonely)" appearing under Albums).
5. **Client-side album dedup is too weak** — uses only `toLowerCase().trim()`, missing bracket-suffix variants like "A Great Chaos" vs "A Great Chaos (Deluxe)".
6. **MusicBrainz timeouts** — 10s httpx timeout is too short for MB's slower `release-group` and `album-tracks` endpoints, causing intermittent `ReadTimeout` errors.

## User value

- Tapping a SoundCloud set opens its tracklist instead of showing an error.
- Artist discography sections show clean, correctly-classified entries without type=None noise or duplicate albums.
- Searching for a well-known song reliably surfaces the original artist's version first.
- Album names on track detail are consistent regardless of when you search.

## Scope tier / MVP cut

- **Minimal (ship this):** All 6 fixes below. The ranking improvement is iterative — Phase 1 (sort reorder) ships; Phase 2 (artist-consensus) ships only if eval harness shows remaining gaps.
- **Deferred to post-launch:** Server-side Redis caching for content APIs (AC#24-25 from v4 spec), Discogs provider, cross-provider top-track merge.
- **Justified exceptions:** None.

## Acceptance criteria

### SoundCloud set fixes

1. **AC#1** — Given a SoundCloud artist with sets, when loading artist albums, then each set's `record_type` defaults to `"album"` (not `"ep"`).
2. **AC#2** — Given a SoundCloud set whose title contains `playlist`, `mix`, `best of`, or `compilation` (case-insensitive), when loading artist albums, then that set is excluded from results.
3. **AC#3** — Given a SoundCloud set, when tapping it on the artist detail screen, then the set's tracklist loads and displays (the SoundCloud adapter implements `AlbumContentProvider.get_album_tracks`).
4. **AC#4** — Given a SoundCloud set with no artwork in the listing response, when the client has already loaded albums from other providers (Deezer/MB/Last.fm), then the set's `image_url` is back-filled from a title-matched album from another provider.

### MusicBrainz type=None filtering

5. **AC#5** — Given a MusicBrainz release-group with `primary-type` absent or null, when loading artist albums, then that entry is excluded from the response.

### Client-side dedup normalization

6. **AC#6** — Given two albums with titles differing only by a bracketed suffix (e.g., "A Great Chaos" vs "A Great Chaos (Deluxe)"), when the client dedup runs in `useArtistContent`, then they merge into one entry (keeping the one with higher `track_count`).

### Album-name determinism

7. **AC#7** — Given two merged search results where the canonical provider has `extras["album"] = None` and the other has a non-None album name, when the merge completes, then the non-None album name is preserved (not overwritten by None).
8. **AC#8** — Given the same search query executed multiple times, when provider response timing varies, then the `extras["album"]` value on merged results is deterministic (highest-prior non-None value always wins).

### MusicBrainz timeout resilience

9. **AC#9** — Given the MusicBrainz httpx client, when configured in `wiring.py`, then the timeout is 20 seconds (up from 10).
10. **AC#10** — Given a MusicBrainz content API call (album tracks or artist albums) that times out, when a retry is attempted, then the retry uses the same timeout and returns the result if it succeeds on the second attempt.

### Search ranking: Phase 1 — multi-source promotion

11. **AC#11** — Given the `fuse_and_rank` sort key, when ordering results within a relevance band, then `multi_source` (cross-provider agreement) is evaluated before `popularity`.
12. **AC#12** — Given the `rerank` sort key, when re-ordering after enrichment, then `multi_source` is evaluated before `popularity` (same order as `fuse_and_rank`).
13. **AC#13** — Given the eval harness corpus with a new "super shy" → NewJeans test case, when Phase 1 sort reorder is applied, then overall `hit@1` does not regress and the "super shy" case achieves HIT@3 or better.

### Search ranking: Phase 2 — artist-consensus boost (conditional)

14. **AC#14** — Given a relevance band with 3+ results sharing the same normalized title but different artists, when one artist appears on more provider sources than others, then that artist's result gets a tiebreak advantage (artist-consensus boost) within the band.
15. **AC#15** — Given the eval harness after Phase 2, when the "super shy" test case is evaluated, then the NewJeans version achieves HIT@1.
16. **AC#16** — Phase 2 is implemented **only if** Phase 1 eval results show remaining ranking gaps for the target test cases. If Phase 1 achieves HIT@1 for all target cases, Phase 2 is skipped.

## Out of scope

- **SoundCloud set type heuristics** — no track-count-based album/EP classification. All sets default to `album`; noise filtered by title keywords.
- **Full `normalize_for_match` port to TypeScript** — client dedup only needs bracket-stripping + lowercase, not the full 8-step normalization.
- **Ranking overhaul beyond Phases 1-2** — this spec tunes the existing pipeline; a ground-up ranking redesign (ML scoring, etc.) is a future effort.
- **Content API retry for non-timeout errors** — only `ReadTimeout` gets a single retry; other errors (rate-limit, connection refused) follow existing circuit-breaker behavior.
- **SoundCloud set artwork extraction** (dropping `extract_flat`) — too slow; artwork is back-filled from other providers instead.

## Design considerations

- [vault: wiki/concepts/Hexagonal Architecture.md] — SC `get_album_tracks` is a new port implementation on an existing adapter; no domain changes needed.
- [vault: wiki/concepts/Repository Pattern.md] — not applicable; this spec touches adapters and application-layer dedup logic, no persistence changes.

High-level approach:

- This is a **read** path across the `discovery` bounded context.
- It does **not** require new aggregates or value objects — all changes are in existing adapters (`soundcloud/adapter.py`, `musicbrainz/adapter.py`), application logic (`dedup.py`, `get_artist_content.py`), platform config (`wiring.py`), and mobile hooks (`useArtistContent.ts`).
- It does **not** introduce new external dependencies.

### SoundCloud `get_album_tracks` implementation

The adapter receives a set identifier (the set's URL path or ID from `SourceRef.external_id`). Implementation extracts `https://soundcloud.com/<set-url>` via yt-dlp (same `_extract` pattern used for top tracks), translates entries to `SearchResult` tracks with `track_number` and `duration_seconds` in extras. The adapter class gains `AlbumContentProvider` alongside its existing `ArtistContentProvider`.

### Client-side artwork back-fill

In `useArtistContent.ts`, after merging SC albums with Deezer/MB/Last.fm albums via `dedupAlbumsByTitle`, a second pass checks SC results with no `image_url`. For each, it searches the pre-dedup album list for a title match (using the same normalized key) from another provider and copies the `image_url`. This is a client-side concern — no backend changes.

### Extras merge fix

In `_merge()` in `dedup.py`, replace `extras = {**other.extras, **canonical.extras}` with a key-by-key merge that prefers non-None values from the canonical provider, falling back to the other provider's value when canonical's is None. This preserves the prior-based hierarchy while preventing data loss.

### Ranking sort reorder

In both `fuse_and_rank` and `rerank`, the sort key tuple changes from:

```
(-band, demoted, bootleg, -popularity, -rrf, -multi_source, -prior, artist, title)
```

to:

```
(-band, demoted, bootleg, -multi_source, -popularity, -rrf, -prior, artist, title)
```

This promotes cross-provider agreement above popularity — originals appearing in 3+ providers outrank single-source covers even when the cover has higher play counts.

### Artist-consensus boost (Phase 2, conditional)

If Phase 1 eval shows gaps: within each relevance band, group results by normalized title and count distinct providers per artist. The artist appearing on the most providers for that title gets `artist_consensus = 0` (boosted); others get `1`. This inserts between `multi_source` and `popularity` in the sort key.

## Dependencies

- **Bounded contexts**: discovery (existing)
- **Other features**: discover-music-v4 (shipped — this spec builds on it)
- **External services**: SoundCloud via yt-dlp (existing), MusicBrainz API (existing)
- **Library/framework additions**: none

## Risks / open questions

- **Risk**: Promoting multi-source above popularity could hurt ranking for new releases that are only indexed by one provider so far — mitigation: the eval harness includes recent tracks; measure regression before shipping.
- **Risk**: SoundCloud `get_album_tracks` via yt-dlp may be slow for large sets (50+ tracks) — mitigation: default limit parameter, same timeout as other SC calls.
- **Risk**: Title-keyword filter for SC sets ("playlist", "mix") may over-filter legitimate releases with those words in the title — mitigation: the filter is case-insensitive whole-word only; "remix" is NOT in the filter list (remixes are legitimate releases).
- **Open question**: Does the eval harness need new query categories for "popular song by bare title" (no artist specified)? — to resolve via: add 5-10 test cases and baseline before implementing ranking changes.

## Telemetry

- **Log events**: `soundcloud_set_filtered` (when a set is excluded by title-keyword filter), `mb_release_group_filtered` (when a type=None entry is excluded), `mb_content_retry` (when a timeout retry fires).
- **Metrics**: SC `get_album_tracks` latency, MB content API retry rate, eval harness hit@1/hit@3/MRR per category (tracked across ranking phases).
- **Alerts**: none (pre-launch, solo user).

## Related

- `docs/specs/discover-music-v4/spec.md` — predecessor (artist content, navigation, search UX)
- `docs/specs/discover-music-v3/spec.md` — enrichment pipeline, cover/bootleg burial
- `docs/adr/0007-ranking-overhaul.md` — ranking pipeline design decisions
- `scripts/discovery_eval/` — eval harness for ranking regression testing

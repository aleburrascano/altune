# Spec: discover-music-v4 — discovery UX polish, multi-provider artist detail, navigation overhaul

> Spec for `discover-music-v4` — version 1, drafted 2026-06-07.
> Authors: solo + Claude.
> Status: Draft.

## Problem

The discovery feature (v1–v3) built a solid search engine but the **detail screens and search UX** have rough edges that break immersion:

1. **Recent searches vanish** — chips don't persist or display after app restart.
2. **Back navigation is broken** — pressing back from a track detail returns to the discover search screen instead of the artist page the user came from.
3. **Duplicate albums** — artist detail shows the same album twice (e.g., Ken Carson "More Chaos" with 21 vs 22 tracks, Lucki "s*x m*ney dr*gs") because no dedup is applied to single-provider artist album responses.
4. **Missing discography types** — artist detail only shows "Albums"; singles, EPs, and compilations are absent despite the data being available in `extras.record_type`.
5. **Single-provider artist detail** — only the highest-priority provider (Deezer) is queried for an artist's discography. Platform-exclusive releases (e.g., Ken Carson's "Lost Files" EPs on SoundCloud, cataloged by Last.fm) never surface.
6. **Album card misalignment** — when album titles wrap to a second line, the year shifts down and cards in the horizontal row look visually inconsistent.
7. **No search input clear button** — clearing a search requires manual backspace; no X affordance.
8. **No as-you-type search** — results only fire on keyboard submit, making discovery feel sluggish.
9. **Album detail lacks metadata** — no year, track count, or total duration shown.

## User value

Discovery feels polished and responsive: search updates as you type, recent searches persist reliably, artist pages show complete multi-provider discographies sectioned by type, navigation lets you drill arbitrarily deep and back out naturally, and album details surface useful metadata at a glance.

## Scope tier / MVP cut

- **Minimal (ship this):** All 9 items above. This is a polish pass on an existing feature — each item is small and well-scoped.
- **Deferred to post-launch:** Discogs provider integration, SoundCloud artist page scraping (adapter stubbed only), streaming/real-time search suggestions, playback from detail screens.
- **Justified exceptions:** Content API caching (Redis) — needed now because the multi-provider artist detail triples the number of provider calls per artist visit.

## Acceptance criteria

### Bug fixes

1. **AC#1 — Recent searches persist** — Given a user searches "Ken Carson" and navigates away, when they return to the discover screen, then the "Ken Carson" chip appears in recent searches.
2. **AC#2 — Recent searches on fresh mount** — Given a user searched in a previous app session, when the app restarts and they open discover, then their most recent searches appear as chips.
3. **AC#3 — Album dedup by title** — Given an artist has multiple releases with the same title (e.g., 21- and 22-track versions), when the artist detail loads, then only the version with the most tracks appears.
4. **AC#4 — Album card shows track count** — Given an album card in artist detail, then it displays artwork, title, year, and track count (e.g., "22 tracks").

### Navigation

5. **AC#5 — Back returns to previous screen** — Given discover → artist → track navigation, when the user presses back from track detail, then they return to the artist page.
6. **AC#6 — Unlimited stack depth** — Given a navigation chain of 5+ screens (discover → artist → album → track → another artist → ...), when the user presses back repeatedly, then each press returns to the previous screen in order.
7. **AC#7 — Tab press resets stack** — Given the user is 3+ screens deep in the discover stack, when they tap the Discover tab icon in the tab bar, then the stack resets to the search screen.
8. **AC#8 — Cold start fallback** — Given a deep link or app restart with no navigation state, when the detail screen has no handoff data, then it redirects to `/discover`.

### Search UX

9. **AC#9 — Debounced as-you-type** — Given the user types "ken" in the search input, when 300ms pass with no further keystrokes and the input has 2+ characters, then search results load automatically.
10. **AC#10 — Debounce reset** — Given the user types "ken" then continues typing "ken car" within 300ms, then the timer resets and search fires 300ms after the last keystroke.
11. **AC#11 — Enter bypasses debounce** — Given the user types "ken" and immediately presses Enter, then search fires immediately without waiting for the debounce timer.
12. **AC#12 — Clear button appears** — Given text is present in the search input, then a circular X icon is visible on the right side of the input.
13. **AC#13 — Clear button hidden when empty** — Given the search input is empty, then no X icon is shown.
14. **AC#14 — Clear resets to recent searches** — Given the user has search results visible, when they tap the X button, then the input clears, committed query resets, and recent searches chips are shown.
15. **AC#15 — Immediate loading skeleton** — Given the user types 2+ characters, then the recent searches view is immediately replaced by a loading skeleton (before results arrive).

### Artist detail expansion

16. **AC#16 — Multi-provider album merge** — Given an artist detail page, then albums are fetched from Deezer, MusicBrainz, and Last.fm in parallel and merged/deduped.
17. **AC#17 — Discography sections by type** — Given an artist has albums and singles, then separate horizontal scroll sections appear: "Albums", "Singles", "EPs" (each only if the artist has releases of that type).
18. **AC#18 — Section ordering** — Given an artist with albums, singles, and EPs, then sections appear in order: Albums, Singles, EPs.
19. **AC#19 — Release date sort within section** — Given a discography section, then releases are sorted newest-first by release date.
20. **AC#20 — SoundCloud adapter stub** — Given the SoundCloud content adapter, then `get_artist_albums` exists but raises `NotImplementedError`. The multi-provider merge catches this and proceeds with results from the other providers (no runtime errors, no user-visible failure).
21. **AC#21 — Album card alignment** — Given album cards of varying title lengths in a horizontal row, then text is left-aligned with year hugging the title naturally (no fixed-height title area; cards may have different total heights).

### Album detail polish

22. **AC#22 — Album metadata row** — Given an album detail screen, then a metadata summary appears between the hero and tracklist showing year, track count, and total duration (e.g., "2024 · 22 tracks · 1 hr 12 min").
23. **AC#23 — Duration format** — Given an album with total duration under 1 hour, then it shows as "45 min". Given over 1 hour, then "1 hr 12 min".

### Backend hardening

24. **AC#24 — Content API caching** — Given a second visit to the same artist detail within the cache TTL, then the response is served from Redis cache (matching provider-specific TTLs: Deezer 6h, MusicBrainz 24h, Last.fm 12h).
25. **AC#25 — Cache key structure** — Given a content API request, then the cache key follows `content:v1:{endpoint}:{provider}:{external_id}`.

## Out of scope

- **Discogs provider** — revisit if catalog gaps remain after the 3-provider merge ships.
- **SoundCloud artist page scraping** — adapter stubbed only; full implementation is a separate spec.
- **Playback from detail screens** — separate `playback` bounded context, not in scope.
- **Search suggestions / autocomplete** — as-you-type fires the full search, not a lighter suggestions endpoint.
- **Infinite scroll / pagination** on artist discography — current limits (30 per provider) are sufficient for v1.
- **Cross-provider merge for top tracks** — top tracks remain single-provider (Deezer priority) for now.
- **Track detail screen changes** — no new fields or layout changes to the track detail view.

## Design considerations

- [vault: wiki/concepts/Vertical Slice Architecture.md] — each concern group (bug fixes, navigation, search UX, artist detail, album detail, caching) is an independent vertical slice. No slice depends on another.
- [vault: wiki/concepts/Repository Pattern.md] — search history persistence uses the existing `SearchHistoryRepository` port. The bug fix investigates the insert path, not the pattern.
- [vault: wiki/concepts/Hexagonal Architecture.md] — multi-provider album merge lives in the application layer (`GetArtistAlbums` use case), not in adapters. Adapters remain single-provider; the use case orchestrates and merges.

**High-level approach:**

- Bug fixes are **investigation + fix** — the recent searches issue needs root-cause analysis at implementation time (auth token? silent DB failure? query key mismatch?).
- Navigation overhaul restructures Expo Router from flat `(tabs)/detail` to a **nested stack within the discover tab**. Detail becomes a stack screen under discover, not a sibling tab route.
- Search debounce is pure frontend — new `useDebounce` hook (or inline `setTimeout`/`clearTimeout`) wrapping the existing `committedQuery` state.
- Multi-provider artist detail is the biggest backend change: `GetArtistAlbums` use case fans out to all providers, merges results using title-normalized dedup (same JW approach as `fuse_and_rank` in search), then groups by `record_type`.
- Content API caching follows the existing search caching pattern in `_call_provider_with_cache`.

## Dependencies

- **Bounded contexts**: `discovery` (existing)
- **Other features**: none — this builds on shipped discover-music-v1/v2/v3
- **External services**: Deezer, MusicBrainz, Last.fm (existing); Redis (existing, for new cache keys)
- **Library/framework additions**: none

## Risks / open questions

- **Risk**: Recent searches bug may have multiple root causes (auth, DB, query hook) — mitigation: investigate with logging before fixing.
- **Risk**: Multi-provider album merge may surface low-quality Last.fm entries (crowd-tagged, wrong artist) — mitigation: apply the same match gate and JW thresholds used in search dedup.
- **Risk**: Navigation restructuring (tabs → nested stack) may affect the library tab's detail navigation — mitigation: test both tabs' navigation flows end-to-end.
- **Risk**: Expo Router nested stack within tabs may have platform-specific quirks (Android back button, iOS swipe-back gesture) — mitigation: test on both platforms.
- **Open question**: Should compilations appear in their own section or be grouped under Albums? — resolved: group under Albums (compilations are usually label-curated, not artist-driven).

## Telemetry

- **Log events**: `artist_detail_multi_provider_merge` (provider count, merge stats, duration), `content_cache_hit` / `content_cache_miss` (provider, endpoint, key), `search_history_persist_failed` (already exists, verify it fires).
- **Metrics**: content API cache hit rate per provider, artist detail load latency (before/after caching), search-to-first-result latency with debounce.
- **Alerts**: none for now (pre-launch).

## Related

- `docs/specs/discover-music-v1/spec.md` — original discovery spec
- `docs/specs/discover-music-v2/spec.md` — multi-kind search, sectioned UI
- `docs/specs/discover-music-v3/spec.md` — enrichment scoring, uniform popularity
- `docs/specs/view-result-detail/spec.md` — detail screen (artist/album/track bodies)
- `docs/adr/0007-discovery-search-architecture.md` — search architecture decisions

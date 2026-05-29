# Spec: discover-music-v2 — multi-kind, relevance + popularity ranked search

Status: Draft (brainstormed 2026-05-28). Supersedes the ranking + scope of `discover-music-v1`; builds on ADR-0007 (+ its ranking-overhaul addendum).

## Problem

Discovery search felt broken across three attempts. The decisive cause, found by searching the user's real query "che rest in bass": **the app only searches tracks.** Every provider adapter returns nothing for album/artist kinds and only calls the track endpoint [VERIFIED:Read@services/api/src/altune/adapters/outbound/discovery/deezer/adapter.py#L50-L59]. The user was looking for the **album "REST IN BASS" by Che** [rest in bass — wikipedia](https://en.wikipedia.org/wiki/Rest_in_Bass) — which exists on Apple/Deezer/Last.fm but can never appear, because no album endpoint is ever called. Three rounds tuned the *sort order of a candidate list that structurally could not contain the answer.*

Secondary: even for tracks, ranking ignored **popularity** — the signal that made the legacy `music-manager` "feel" right (it ranked by Deezer `rank`, log-scaled [VERIFIED:Read@C:\Users\Alessandro\music-manager\backend\services\search_service.py#L294-L344]).

## User value

Type an artist, album, song, or a mix → get a Spotify/iTunes-style result screen: a **Top Result**, filter chips (**All · Albums · Songs · Artists**), and **Albums / Songs / Artists** sections, ranked by how well they match *and* how popular they are. "che rest in bass" returns the album **REST IN BASS by Che** as the Top Result.

## Scope tier (MVP cut)

**In:**
- Album + artist search added to Deezer, iTunes, MusicBrainz, Last.fm (SoundCloud stays tracks-only — `scsearch` can't do albums/artists).
- Popularity signal (cross-provider, real numbers).
- Relevance-gated, popularity-ordered ranking; Top Result + capped sections.
- Cross-provider merge for albums/artists.
- Sectioned mobile UI with a loading **skeleton**.
- Tapping a result **records a click only** (no navigation — playback/detail/library screens don't exist yet).

**Out (deferred):**
- **Playlists — removed entirely** (not deferred): drop `ResultKind.PLAYLIST` and all references.
- True incremental result streaming (skeleton now; streaming is a named fast-follow slice).
- Dedicated Last.fm popularity-enrichment lookup for obscure items.
- Tap navigation / detail / playback / add-to-library.
- As-you-type (ADR-0007 keeps submit-only v1).

## Providers × kinds matrix

| Provider | Track | Album | Artist | Popularity signal |
|---|---|---|---|---|
| Deezer | `/search/track` | `/search/album` | `/search/artist` | track `rank`, artist `nb_fan` |
| iTunes | `entity=song` | `entity=album` | `entity=musicArtist` | none (artist results also lack artwork) |
| MusicBrainz | `/recording` | `/release-group` | `/artist` | none |
| Last.fm | `track.search` | `album.search` | `artist.search` | `listeners` (track/artist) |
| SoundCloud | `scsearch` | — | — | none (position only) |

Fan-out: one parallel task per (provider, kind) via `asyncio.gather`, reusing the existing per-source 1500ms timeout, circuit breakers, bulkhead clients, and Redis cache. ~13 tasks run concurrently → wall-clock ≈ slowest single call, capped by the 2000ms budget; slow sources drop to `partial: true`. Rate limits (iTunes ~20/min, MB ~1/s) absorbed by cache + graceful `rate_limited`.

## Ranking model — relevance gates, popularity orders

Per merged result:
- **Match gate (parameter-free — no tunable floor):** a result is kept only if it shares **at least one content token** with the query, where content tokens are the normalized query tokens minus a fixed stopword set (`the, a, an, in, of, and, to, …`) and length-1 tokens. Zero-overlap results are dropped. This replaces the earlier magic `0.50` float: the rule is definitional and explainable ("we don't show results that share no words with your search"), not a hand-tuned knob. If nothing passes the gate → zero-results.
- **Relevance** `rel ∈ [0,1]` = rapidfuzz `token_sort_ratio` of `query_norm` vs the result's matchable text, best of: title, artist, and the kind's combined form (track → "artist title", album → "artist album", artist → name). Banded to 0.1. Used only for *ordering* the gated results — never as a cutoff.
- **Popularity** `pop ∈ [0,1]` = best *real* signal across the merged entry's sources — Last.fm `listeners` and Deezer `rank`/`nb_fan`, log-normalized (Last.fm/Deezer act as popularity oracles). Falls back to the provider's native list position only for entries no popularity-bearing provider returned.
- **Agreement** = RRF over distinct providers (existing).

Sort key (DESC): `relevance_band → popularity → RRF agreement → winning prior → alpha`.

The only fixed inputs are a standard stopword list and the ordering precedence above — both definitional, neither a tuned magnitude. There is no relevance threshold to calibrate.

**Top Result** = the single highest-ranked entry across all kinds, with a **kind-priority tiebreak on near-equal relevance: Artist > Album > Track** (so "che" headlines the artist Che, an album-name query headlines the album).

Confidence is **no longer displayed** — the badge is removed (see Mobile UX). Confidence is still computed internally (cheap; retained for telemetry / future CTR-by-confidence analysis) but has no UI or ranking role.

## Merge / dedup per kind

- **Tracks:** unchanged (ISRC, else JW≥0.85 on normalized artist|title).
- **Albums:** merge on normalized `(artist, album-title)` JW≥0.85 (no ISRC at album level).
- **Artists:** merge on normalized artist name JW≥0.92.
Merged entry keeps all `SourceRef`s; canonical representative by highest per-source prior (existing rule); artwork/popularity filled from the best source that has them.

## Domain / contract impact

- `ResultKind`: **remove `PLAYLIST`**; members become `ARTIST | ALBUM | TRACK`. Glossary + `test_result_kind` update accordingly.
- `SearchResult` is already kind-agnostic — **no aggregate change.** Album/artist data rides in `extras` (album → `year`, `track_count`; artist → none required) + `popularity`.
- Wire contract `results[]` unchanged (each item already carries `kind`); the mobile client groups by kind and derives the Top Result. No `/v2` route needed.

## Mobile UX (`apps/mobile/src/features/discover/`)

- **Loading:** animated **skeleton rows** immediately on submit (never blank) — `testID="discover-loading"`.
- **Filter chips** at the top, Spotify-style, in this order: **All · Albums · Songs · Artists** (no Playlists). "All" is the default blended view; tapping a kind chip filters to just that kind (full list for that kind).
- **Blended "All" view:** a **Top Result** card, then **Albums / Songs / Artists** sections (that order), **≤10 each** with a **"See all"** affordance that switches to that kind's filter chip. Empty sections hidden. Song row = existing (minus the confidence dot); artist row = circular avatar; album row = square art + year. ("Albums" includes singles & EPs — Spotify groups release types under albums.)
- **No confidence badge:** remove `ConfidenceDot` and the confidence-driven "verified glow" from result rows/cards entirely.
- **Tap:** records a click via the existing `POST /v1/discovery/clicks` (works for any kind); no navigation.
- **States:** partial banner (existing), zero-results (nothing passes the match gate across all kinds), full-error (existing).

## Acceptance criteria (verified in the app via the search bar — primary)

1. Searching **"che rest in bass"** returns the album **REST IN BASS by Che** as the **Top Result** (album kind), in the app.
2. Searching an **artist name** (e.g. "che") headlines the **artist** (Artist > Album > Track tiebreak); searching a **song title** headlines the track.
3. Albums and artists appear in their own sections; the same album/artist from multiple providers shows **once** (merged).
4. A nonsense query surfaces only results that share a word with the query, or zero-results — **no irrelevant junk** (e.g. no "Under Pressure" for "che rest in bass"). No tunable threshold is involved.
5. On submit, a **skeleton** shows immediately; results land within ~2s (slow sources → partial, never a >2s blank).
6. Tapping any result records a click (DB row) and does not navigate.
7. Playlists do not appear anywhere; `ResultKind` has no `PLAYLIST`.
8. Filter chips **All · Albums · Songs · Artists** render in that order; tapping one filters to that kind. Each section in "All" caps at 10 with a working "See all".
9. No confidence badge or confidence-driven glow appears anywhere in the UI.

Automated guards (secondary, not the acceptance gate): the `tests/eval/` harness extended with album/artist + popularity cases; adapter integration tests for the new endpoints. **The acceptance check is the live search bar**, per the user.

## Risks

- iTunes/MB rate limits under 3 calls/search — mitigated by cache + circuit breaker; monitor in-app.
- iTunes artist results lack artwork — fill from Deezer/Last.fm on merge; placeholder avatar otherwise.
- Album/artist merge without ISRC relies on JW(name) — tune thresholds against live results.
- Removing `PLAYLIST` touches a sacred test (`test_result_kind`) + glossary — expected, gated edit.

## Verification plan

`/run` the app; search "che rest in bass" (album top result), "rest in bass", "che" (artist headline), a known song, and a nonsense string; confirm sections, merge, skeleton, and no-junk behavior live. Keep `scripts/ranking_eval.py` for raw provider/recall spot-checks.

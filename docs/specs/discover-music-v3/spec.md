# Spec: discover-music-v3 — enrichment-scored, signal-complete ranking

Status: Draft (brainstormed + API-audited 2026-05-28). Builds on discover-music-v2; supersedes its ranking/enrichment. Pairs with the per-provider capability audit (this session).

## Problem

v2 ranks on whatever each provider returns *at search time*, which is uneven:
- iTunes returns **no popularity** at all; MusicBrainz albums return **no art**; an underground artist surfaced only by iTunes/MB has **popularity 0** and loses — so ranking feels source-dependent and arbitrary, and mainstream artists can be out-ranked by their own hit songs on a popularity coin-flip.
- We ignore signals we already receive (MusicBrainz per-result `score`) or could cheaply fetch (Last.fm `getInfo` play counts, which are keyed by `(artist, title)` we already hold post-search).

A full audit of every source (Deezer, iTunes, MusicBrainz, Last.fm, TheAudioDB) showed the win isn't more providers — it's **exploiting what we have**.

## Core architecture: search locates, a cached enrichment pass scores

Two phases:
1. **Locate** — scatter-gather search across providers (as today) to get candidate identities + native order. Adopt **fielded queries** for sharper recall.
2. **Enrich + score** — after merge, run ONE **bounded (top ~25), concurrency-capped, cached** enrichment pass keyed by `(artist, title)` / `MBID` that back-fills the SAME signals onto every result regardless of which provider surfaced it. This is inherently **uniform** — mainstream and underground get identical treatment, killing the favoritism/asymmetry problem.

## Signal map (live-verified by the audit)

**Relevance**
- **Own-identity scoring** (the uniform headline fix): score a result by `token_sort_ratio(query, <its own identity>)` — artist→name, album→"artist album", track→"artist title". Drop the standalone artist-field match so a song can't tie its artist at band 1.0. Any exact name headlines its artist (mainstream or underground); any title headlines its song.
- **MusicBrainz `score`** (0–100, already in every MB search response) blended as a relevance signal.
- **Fielded queries** with single-string fallback: when the query parses as artist+title, also issue Deezer `q=artist:"x" track:"y"` and MB `artist:x AND recording:y` (+ `status:official`); union the results. (Deezer `order=` is a no-op — removed.)

**Popularity (uniform, the big lever)**
- **Last.fm `getInfo`** → `playcount` + `listeners` for **track / album / artist**, keyed by `(artist[, title])` we hold post-merge. Primary cross-source popularity for *every* result. (`*.search` has no playcount — `getInfo` is the source.)
- Secondary/native: Deezer track `rank`, artist `nb_fan`; TheAudioDB `intTotalPlays`/`intScore`. Used when Last.fm misses.
- Log-normalized to [0,1]; `extras["popularity"]` = best available.

**Images (hi-res, never empty when avoidable)**
- Deezer: build hi-res from `md5_image` (≤1800 verified) instead of `cover_xl`.
- iTunes: rewrite artwork to `1000x1000bb`.
- MusicBrainz albums: **Cover Art Archive** `release-group/<mbid>/front-500` (MBID is already inlined in MB search results → zero extra MB calls).
- TheAudioDB: artist art (thumb/fanart) + album art, MBID-keyed.
- **Never** Last.fm artist images (gray-star placeholder).
- `ChainedArtworkResolver` extended: Deezer → CAA(MB albums) → TheAudioDB, skipping placeholders.

**Dedup**
- **MBID as the universal join** (MB search inlines it; Last.fm getInfo + TheAudioDB are MBID-keyed) + existing ISRC + JW. Merge the same entity across all providers.

**Disambiguation**
- Demote non-primary releases: Deezer `record_type` (single/compilation), MB `primarytype:album`/`status:official`. Genres/tags (Last.fm `toptags`, MB `genres`, TheAudioDB `strGenre`) carried in `extras` for future filtering.

## Ranking (final sort key)

`relevance-band (own-identity token_sort, blended with MB score) → popularity (uniform, Last.fm-primary) → cross-provider agreement (RRF) → record-type/primarytype demotion → alpha`. Parameter-free match gate unchanged. No kind hierarchy. No favoritism.

## Enrichment pass — design & limits

- Runs in the use case after `fuse_and_rank`, on the **top ~25** results only.
- **Concurrency-capped** (e.g. 8) + **cached** (reuse per-source TTLs; getInfo + CAA + TheAudioDB responses are highly cacheable — popularity drifts slowly).
- **Best-effort**: any enrichment failure leaves the result as-is; never fails the search.
- Rate-limit aware: MB 1 req/s (enrichment avoids MB — CAA art needs no MB call since MBID is in hand), Last.fm ~5/s, TheAudioDB 30/min + free-key list truncation.
- New application ports: `PopularityResolver` (Last.fm getInfo), reuse/extend `ArtworkResolver`. Adapters implement them; wiring composes.

## Acceptance criteria (verified in the app — primary)

1. Searching an **artist name** (mainstream *and* underground) headlines that **artist**; a **song title** headlines the song; an **album name** the album — uniformly, no source/popularity favoritism.
2. Every shown result that has a real cover **shows it** (Deezer hi-res, iTunes 1000px, CAA for MB albums, TheAudioDB for artists); art-less is rare and graceful.
3. Popularity is present on results regardless of which provider surfaced them (Last.fm getInfo back-fill) — an iTunes/MB-only mainstream artist still ranks correctly.
4. The same entity from multiple providers shows once (MBID/ISRC/JW merge).
5. Search returns within budget; the enrichment pass is bounded and never makes the search fail or exceed ~2× the pre-enrichment latency on a cold call.
6. "che rest in bass" → album top result; "creep" → the song; an artist query → the artist.

Secondary guards: expanded `tests/eval/` (artist-name, underground, song, album, partial, nonsense) + adapter/enrichment unit+integration tests. The acceptance gate is the live search bar.

## Risks

- **Enrichment latency / rate limits** — mitigated by top-N bound, concurrency cap, caching, and best-effort fallthrough.
- **Last.fm getInfo coverage** — niche tracks may 404; fall back to native popularity (Deezer rank) or 0.
- **MBID coverage** — not every result has an MBID; ISRC/JW remain the fallback dedup.
- **TheAudioDB free key** truncates list endpoints + misses new/niche artists — used only for art/MBID enrichment, not ranking.
- **Complexity** — the enrichment pass is the main new moving part; keep it one well-tested application method with injected resolver ports.

## Verification plan

`/run` the app; search a mainstream artist, an underground artist, a song, an album, "che rest in bass", and a nonsense string; confirm headline correctness, covers filling, and popularity-sane ordering. `scripts/ranking_eval.py` for raw ranking; new enrichment unit tests for the back-fill.

# discovery application — bounded-context local rules

Use cases + ports for the unified music search. `SearchMusic` is the load-bearing use case — scatter-gather over 4 providers, dedup + rank, persist history. `RecordClick`, `ListSearchHistory` are smaller surface use cases. All adapter wiring lives in [services/api/src/altune/platform/wiring.py](../../platform/wiring.py).

## Key terms

- **SearchProvider** — `Protocol` adapters implement (deezer, musicbrainz, soundcloud, lastfm) [VERIFIED:Read@c:\Users\Alessandro\Desktop\altune\services\api\src\altune\application\discovery\ports.py]. Two methods: `search(query, kinds, limit)` returns `ProviderSearchResponse`; `lookup_by_url(url)` returns `SearchResult | None`.
- **QueryCache** — `Protocol` the Redis adapter implements. `get(provider, query_norm, kinds)` returns `(results, fetched_at) | None`; `set(... ttl)`. Errors degrade to None (caller falls through to live).
- **ClickInsertOutcome** — `(inserted: bool, deduped_against_id: UUID | None)`. Sliding-window dedup result from `SearchClickRepository.insert_if_outside_window`.
- **Scatter-gather** — `asyncio.gather(*tasks)` with per-task try/except so siblings survive any one provider's failure [VERIFIED:Read@c:\Users\Alessandro\Desktop\altune\services\api\src\altune\application\discovery\search_music.py#L122-L139]. Each OK provider's results are kept as their own **group** (native relevance order preserved) and passed to `fuse_and_rank`, not flattened.
- **`fuse_and_rank(per_provider, query_norm)`** — relevance-first ranker (ADR-0007 ranking-overhaul addendum). PRIMARY signal is a **continuous query-relevance score** (`_relevance_score`: rapidfuzz `token_sort_ratio` of the query vs title / artist / "artist title", best wins) banded to 0.1; a **relevance floor** (`_REL_FLOOR=0.50`) drops non-matches so unsatisfiable queries don't surface provider-rank junk. Within a band, **RRF** (`Σ 1/(60+best_rank)` over *distinct* providers — same-provider dupes can't inflate) then multi-source then prior break ties. **Confidence is NOT a sort term** — display badge only; a same-provider-only merge stays LOW. Replaced the legacy `dedup_and_rank` (deleted). Per-source priors: MB 0.95, Deezer/iTunes 0.85, Last.fm 0.80, TheAudioDB 0.78, SoundCloud 0.65. Regression-guarded by `tests/eval/` (golden set spans clean/partial/typo/artist-only/nonsense + a no-junk invariant).
  - **`token_sort_ratio`, not `token_set_ratio`**: token_set returns 100 whenever the title is a *subset* of the query, so every same-title result ("Africa — Toto", "Africa — Kidjo") would tie at 100. token_sort penalizes missing/extra tokens — the discrimination the floor/band depend on. Don't "simplify" it back.
- **result_signature** — `sha256(f"{kind}|{normalize(title)}|{normalize(subtitle)}")[:12]` [VERIFIED:Read@c:\Users\Alessandro\Desktop\altune\services\api\src\altune\application\discovery\record_click.py#L46-L51]. Diacritic variants collapse to the same hash.

## Patterns specific here

- **SearchMusic instance is request-scoped** in the router today — built fresh per request. Circuit breakers live on the instance, so they DON'T persist across requests in v1. Acceptable trade-off; a future refactor moves breakers to app.state if telemetry shows resets thrashing.
- **`_call_provider_with_cache` is the only entry the gather tasks touch** — cache check, then `_call_provider`, then optional cache write. All three operations swallow exceptions per the partial-results contract.
- **Cache TTLs are per-source** — MB 24h, Last.fm 12h, Deezer 6h, SoundCloud 1h. Defined at module level in `_DEFAULT_TTLS` [VERIFIED:Read@c:\Users\Alessandro\Desktop\altune\services\api\src\altune\application\discovery\search_music.py#L43-L48]; overridable via the `cache_ttls` field for tests.
- **Rate-limited NEVER counts toward the circuit breaker.** OK = `record_success()`; ERROR/TIMEOUT = `record_failure()`; RATE_LIMITED = no-op. Per AC#5b. The TIMEOUT path also calls `record_failure()`.
- **`url_router.match_provider(query)` is the only URL-detection gate.** SearchMusic's `_execute_url_lookup` is called when match returns a provider; unsupported hosts fall through to scatter-gather text search (AC#10a).
- **History persist is best-effort.** Wrapped in try/except; failures log `search_history_persist_failed` and the search still returns 200.

## view-result-detail catalog browse (AC#14-20)

- **`GetAlbumTracks`** — single-provider fetch, no scatter-gather. Takes `{provider, external_id, limit}`, calls `AlbumContentProvider.get_album_tracks`. Returns `ContentFetchResponse` (provider_name, status, items, latency_ms). Unknown provider → ERROR status, empty items.
- **`GetArtistTopTracks`** — same pattern, calls `ArtistContentProvider.get_artist_top_tracks`. Default limit 5.
- **`GetArtistAlbums`** — same pattern but applies **title-normalized dedup** after the provider call (`_dedup_albums`): normalizes titles via `normalize_for_match`, groups by normalized title, keeps the version with the highest `track_count` in extras. Default limit 10. Multi-provider fan-out is handled client-side (mobile hook calls each provider and merges).
- **`ContentFetchResponse`** — wire output shape for all content-fetch use cases. Mirrors `ProviderSearchResponse` structure (items are `SearchResult` tuples).
- **`AlbumContentProvider` / `ArtistContentProvider`** — port `Protocol`s. Deezer, MusicBrainz, Last.fm implement; iTunes/TheAudioDB skipped (no ID lookups). Adapters translate per-provider DTOs → `SearchResult` same as search.

## Known gotchas

- **`from __future__ import annotations` + dataclass field types in TYPE_CHECKING** can trip ruff's I001 / TC003 lint when the import is only used in a field annotation. Keep `Sequence`, `Mapping`, `QueryCache`, `ProviderName`, `SearchProvider`, `SearchHistoryRepository`, `SearchResult` etc. in the `TYPE_CHECKING` block — they resolve at runtime via string annotations.
- **`fuse_and_rank` uses `rapidfuzz.distance.JaroWinkler`** which mypy lacks stubs for; the `rapidfuzz.*` override in pyproject.toml handles this in batch mypy but per-file mypy hook will complain. Tolerate.
- **JW boundary cases**: 0.85 (merge with MEDIUM), 0.92 (merge with HIGH). Test parametrization explicitly hits 0.84 / 0.85 / 0.91 / 0.92 — don't simplify those tests away.

## discover-music-v2 update

- **Multi-kind search.** The use case requests `{artist, album, track}` (playlist removed). `fuse_and_rank` takes per-(provider) groups and ranks across kinds.
- **Parameter-free match gate** in `dedup.py` (`_passes_gate`): a result is kept only if it shares ≥1 content token (stopwords excluded) with the query. Replaced the old tunable relevance floor — no threshold to calibrate.
- **Sort key:** relevance-band (`token_sort_ratio`, 0.1 buckets) → demotion → bootleg → **multi-source → popularity** → RRF → prior → alpha. Multi-source agreement outranks popularity so originals appearing in 3+ providers beat single-source covers regardless of play counts (discovery-quality-v1). No kind hierarchy (best relevance×agreement wins any kind, so a song query headlines the song).
- **Popularity** rides in `extras["popularity"]` (0–1), max'd across sources in `_merge`. Deezer `rank`/`nb_fan` + Last.fm `listeners`, log-normalized; absent for iTunes/MB.
- **Cover-art back-fill.** `ArtworkResolver` port (`ports.py`); `SearchMusic._enrich_artwork` fills `image_url` for the top art-less results best-effort via the resolver (Deezer adapter). Never fails the search.

## discover-music-v3 update

- **Two-phase: search locates, enrichment scores.** After `fuse_and_rank`, `SearchMusic._enrich` runs a bounded (top `_ENRICH_LIMIT`=25), concurrency-capped (`_ENRICH_CONCURRENCY`=8), best-effort pass that back-fills a UNIFORM popularity onto every result via the `PopularityResolver` (Last.fm `getInfo` play counts, keyed by artist+title) + cover art via `ArtworkResolver`. Then `rerank(results, query_norm)` re-sorts (relevance-band → popularity → multi-source → prior → alpha; no RRF post-merge). This makes an iTunes/MB-only artist rank on the same basis as a Deezer one — no source favoritism.
- **Relevance is own-identity** (`_relevance_score`): artist by name, track/album by "artist title"/title. No bare artist-field match → an exact name headlines its artist (mainstream or underground), a title headlines its song. Uniform.
- **Ports**: `PopularityResolver` (Last.fm), `ArtworkResolver` (Deezer/TheAudioDB chain). Both best-effort, never fail the search.
- **Demotion + MBID dedup** (v3): `_is_demoted` sinks karaoke/tribute/compilation (raw-title markers + `extras["record_type"]` in {compilation}) below the genuine article within a relevance band — it's a tiebreak right after the band, in both `fuse_and_rank` and `rerank`. `_try_merge` merges on an exact `extras["mbid"]` match (MB + Last.fm carry it) before the JW fallback.
- **Cover/bootleg burial fix** (eval-driven, 2026-05-29): copycat uploads that embed the real artist in their TITLE (e.g. `"Blinding Lights The Weeknd (Piano Arrangement)"`) normalize to the full query and land in the genuine track's relevance band, then beat it on popularity. Two coupled fixes in `dedup.py`:
  - **Stopword-insensitive relevance** — `_relevance_score` now also scores on CONTENT tokens (stopwords/articles dropped via `_content_tokens`). `normalize_for_match` strips the leading "The" from "The Weeknd", so a query like `"blinding lights the weeknd"` otherwise scored the genuine `Blinding Lights / The Weeknd` at band 0.9 (article mismatch) while a copycat whose bare title embeds the query hit band 1.0. Content-token scoring is a `max` candidate — it only ever RAISES a score, never lowers — so it can't regress an already-passing case. **Don't drop it back to raw-only; the genuine recording depends on it to reach the copycats' band.**
  - **Expanded demotion markers** — `_JUNK_TITLE_MARKERS` gained `originally performed / performed by / 8-bit / 16-bit`; single-word markers (`cover / arrangement / instrumental / emulation / orgel`) use `_JUNK_TITLE_WORD_RE` with `\b` boundaries so "cover" does NOT fire inside "Undercover". With the genuine now in the same band, demotion is what keeps it on top.
  - **Bootleg-artist demotion** (eval-driven): re-uploads with a *junk artist* subtitle (e.g. `"Blinding Lights The Weeknd" / "Pancadão GD Som"`) carry no marker but cram the real artist into the TITLE. `_is_bootleg` demotes a result whose title swallows the whole query while its artist is foreign to the query — but ONLY when `_genuine_split_exists` finds a real "song-title + queried-artist" split that **exactly covers** the query. The coverage requirement is load-bearing: it stops the rule firing on a bare-title query (where the real artist is legitimately absent) — so a genuine `Song / Artist` on a title-only search is never demoted. It's a tiebreak right after `demoted`, in both `fuse_and_rank` and `rerank`. Measured lift (mainstream, paced): overall hit@1 0.68→0.74, ranking-fail 0.16→0.10.
  - Reproduce/measure with the eval harness: `scripts/discovery_eval/` (batch) and `inspect_query.py` (single-query signal dump).

## discovery-quality-v1 update

- **Multi-source above popularity** (sort key reorder): in both `fuse_and_rank` and `rerank`, `multi_source` moved from position 6 to position 4 (before `popularity`). Originals that appear in 3+ providers outrank single-source covers regardless of play counts.
- **Extras merge: non-None preference** — `_merge` now preserves non-None values from lower-prior sources when canonical has None, preventing MB (high prior, sparse data) from overwriting Deezer's richer extras.
- **Album-name stabilization** — `fuse_and_rank` captures a pre-merge `_album_best` lookup mapping `(signature → album_name)` from the lowest-prior provider (Deezer 0.85 / iTunes 0.85 before MB 0.95). After merging, each accumulated result's `extras["album"]` is replaced with the best album name from this lookup. This prevents MB's often-wrong first-release title (compilations, regional variants) from overwriting Deezer's primary commercial album name. **Don't remove the post-merge stabilization step; without it, over-merging of MB recordings produces random album names.**
- **`save_history` flag** — `SearchMusicInput.save_history` (default `True`) controls whether `_persist_history` runs. Debounced as-you-type queries from the mobile client pass `False` so intermediate partial queries don't bloat the search history chips.

# discovery outbound adapters — bounded-context local rules

ACL adapters for the discovery providers + the Redis cache adapter. Each provider folder is one adapter implementing `SearchProvider` from [application/discovery/ports.py](../../../application/discovery/ports.py). The cache adapter implements `QueryCache`. None of these import each other — siblings coordinate only through the use case.

Providers: `deezer/`, `musicbrainz/`, `soundcloud/`, `lastfm/`, and `itunes/` (added by the ADR-0007 ranking-overhaul addendum — free no-auth iTunes Search API; `https://itunes.apple.com/search?term=&media=music&entity=song`; maps `trackName/artistName/trackId/trackViewUrl`, upscales `artworkUrl100` `100x100`→`600x600bb`, populates `extras.preview_url`; **no ISRC**; `lookup_by_url` returns None since iTunes isn't in the v1 URL-paste set).

## Key terms

- **ACL (Anti-Corruption Layer)** — `[vault: wiki/concepts/Anti-Corruption Layer Pattern.md]`. Per ADR-0007, every provider adapter is one. Deezer/MB/SC/Last.fm DTOs never escape the adapter; the use case sees only `SearchResult`s.
- **Tolerant-reader** — provider response shapes drift; required fields missing → drop the result + log `provider_response_malformed`. Optional fields missing → `None` in `extras`. Unknown fields → ignored. Applied in every `_translate_one_*` helper [VERIFIED:Read@c:\Users\Alessandro\Desktop\altune\services\api\src\altune\adapters\outbound\discovery\deezer\adapter.py#L120-L131].
- **`scsearch` extraction** — SoundCloud's yt-dlp prefix. ADR-0007 strategy revision: SC's Developer API now requires Artist Pro; we use yt-dlp's web extractor instead [VERIFIED:Read@c:\Users\Alessandro\Desktop\altune\docs\adr\0007-unified-music-search.md#L77-L102].

## Patterns specific here

- **Each adapter takes `httpx.AsyncClient` (or `extractor` callable for SC) via constructor.** Bulkhead lives in [platform/wiring.py](../../../platform/wiring.py) — one `AsyncClient` per provider, so a slow Deezer can't drain the Last.fm pool.
- **Per-provider URL bases as module constants** (`_BASE_URL = "https://..."`). Adapter accepts an optional `base_url` override for tests — `respx` fixtures intercept the default URL.
- **HTTP status mapping is uniform**: 429 → `RATE_LIMITED`, any other 4xx/5xx → `ERROR`, network exception → `ERROR`. MB also maps 503 → `RATE_LIMITED` because MB's rate limiter returns 503 not 429.
- **SoundCloud adapter is yt-dlp-based** [VERIFIED:Read@c:\Users\Alessandro\Desktop\altune\services\api\src\altune\adapters\outbound\discovery\soundcloud\adapter.py#L39-L46]; takes two extractors: `extractor` (with `extract_flat='in_playlist'` for search/listing) and `detail_extractor` (without `extract_flat` for full track extraction). Production wraps `yt_dlp.YoutubeDL.extract_info` in `asyncio.to_thread`. Implements `ArtistContentProvider` + `AlbumContentProvider`: `get_artist_top_tracks` and `get_artist_albums` accept an artist name as external_id, resolve it to a SoundCloud username via `scsearch1:<name>` (cached in `_username_cache`), then extract from `soundcloud.com/<username>/tracks` and `/sets`. Sets are mapped to `ResultKind.ALBUM` with `record_type="ep"`. A title-keyword noise filter (`_SET_NOISE_RE`: playlist/mix/best of/compilation) excludes non-album sets. `get_album_tracks` uses the `detail_extractor` + a cached set URL (`_set_url_cache`, populated by `get_artist_albums`) to fetch full track entries from a specific set.
- **yt-dlp options matter**: `extract_flat='in_playlist'` + `ignoreerrors=True` is non-negotiable. Without `extract_flat`, per-track 404s on removed/private tracks cascade-fail the whole scsearch. Captured during C4 fixture work.
- **`lookup_by_url` parses host-specific URL patterns** with `re.match`. Deezer parses `/track/<id>`; MB parses the MBID; Last.fm parses `/music/<Artist>/_/<Track>`; SC passes the URL straight to yt-dlp.
- **v1 supports tracks only across all adapters.** `search()` returns empty `()` when `ResultKind.TRACK not in kinds`. Per-provider artist/album/playlist endpoints are future-scope.
- **Cache TTL is not enforced by the adapter** — `setex` writes with the caller-supplied TTL; Redis enforces expiry. The adapter's `_VERSION_PREFIX = "v1"` is the manual invalidator [VERIFIED:Read@c:\Users\Alessandro\Desktop\altune\services\api\src\altune\adapters\outbound\discovery\cache\redis_cache.py#L24-L25].

## Known gotchas

- **MB User-Agent is non-optional** — without a registered UA with contact info, MB throttles to 1 req/s and may 503. Wiring constructs MB's `AsyncClient` with the UA header from `Settings.musicbrainz_user_agent`; wiring skips MB entirely if UA is unset (rather than spamming the public default). MB client timeout is 20s (higher than other providers' 10s due to MB's slower response times).
- **MB type=None release-groups are filtered** — `_translate_one_release_group` returns `None` when `primary-type` is absent, preventing untyped entries (demos, features, uncategorized releases) from polluting the discography sections.
- **MB content APIs retry once on ReadTimeout** — `get_artist_albums` catches `httpx.ReadTimeout` and retries once before returning `TIMEOUT` status. Other errors (rate-limit, connection) are not retried.
- **Last.fm response shape returns `track` as dict (not list) when there's exactly one result.** The adapter normalizes this to a list before iterating [VERIFIED:Read@c:\Users\Alessandro\Desktop\altune\services\api\src\altune\adapters\outbound\discovery\lastfm\adapter.py#L105-L108].
- **SC fixture uses `t500x500` as the largest-by-width thumbnail**, NOT the `original` entry — the `original` thumbnail in yt-dlp output has no `width`, only `preference`. Tests assert `t500x500 in image_url`.
- **MusicBrainz now requests `inc=isrcs`** (ADR-0007 ranking-overhaul addendum) — `extras["isrc"]` is the first entry of the recording's `isrcs[]` array, or `None` when the recording has no ISRC. This revives the canonical cross-source ISRC merge with Deezer/iTunes; previously MB omitted ISRC and dedup fell back to JW only.
- **`# mypy: warn_unused_ignores = False`** is at the top of every adapter to silence the per-file mypy hook's noise about httpx / sqlalchemy stubs that the batch mypy resolves correctly.

## discover-music-v2 update

- **Album + artist search.** Deezer/iTunes/MusicBrainz/Last.fm each fan out to their album + artist endpoints concurrently inside one `search()` call (per-kind dispatch); SoundCloud stays tracks-only. Each carries an internal per-fetch HTTP-error mapping (e.g. `_DeezerHTTPError`).
- **Popularity** written to `extras["popularity"]`: Deezer track `rank` / artist `nb_fan`, Last.fm `listeners`, log-normalized. iTunes/MB carry none.
- **`DeezerSearchAdapter.resolve_artwork(kind, title, subtitle)`** implements the `ArtworkResolver` port — best-effort cover lookup (no-auth) used to back-fill art-less results (MB items, iTunes artists). Never raises.
- **Last.fm is NOT queried for artists** (resolved): its `artist.search` DB is crowd-scrobbled junk (track/beat titles posing as artists). Last.fm serves only TRACK + ALBUM here; artist entities come from iTunes / MusicBrainz / Deezer. The `_translate_one_artist` helper was removed.
- **TheAudioDB** (`theaudiodb/`, free key "123", 30 req/min): artist-only free-text search (`search.php?s=`) + an `ArtworkResolver` (`resolve_artwork`) for album/track/artist covers (`searchalbum.php`/`searchtrack.php` need artist+title, so they're enrichment-only). High-quality artwork; no popularity. Album/track free-text search is NOT supported (needs the artist name).
- **`artwork.py::ChainedArtworkResolver`** tries resolvers in order (Deezer → TheAudioDB), skipping Deezer's empty-artist placeholder (`d41d8cd9…` md5-of-empty) so it falls through to TheAudioDB's better art.
- **`LastFmSearchAdapter.resolve_popularity(kind, title, subtitle)`** (discover-music-v3) implements `PopularityResolver` via `track/album/artist.getInfo` play counts (log-normalized, `_PLAYCOUNT_MAX_LOG10`). This is the uniform cross-source popularity signal — `*.search` has no playcount. Never raises.
- **v3 signals**: album adapters set `extras["record_type"]` (Deezer `record_type` / iTunes `collectionType` / MB `primary-type`) for demotion. MB release-group results carry `extras["mbid"]` + a **Cover Art Archive** `image_url` (`coverartarchive.org/release-group/<mbid>/front-500`, 307→image or 404, no extra call). iTunes artwork upscales to 1000px.

## view-result-detail catalog browse (AC#14-20)

- **`AlbumContentProvider` / `ArtistContentProvider`** — new port protocols; Deezer, MusicBrainz, Last.fm adapters implement both.
- **Deezer**: `/album/{id}/tracks` + per-track `/track/{id}` contributor enrichment (`_enrich_contributors`, 5-concurrent semaphore), `/artist/{id}/top`, `/artist/{id}/albums`. Most complete — returns image_url, duration, track_position, and featured artists from the contributors array.
- **MusicBrainz**: two-step for album tracks (release-group → first release → recordings via `inc=recordings`). Artist: `recording?artist={mbid}&limit=N&inc=artist-credits` for top tracks (inc required — browse API omits artist-credit by default), `release-group?artist={mbid}` for albums. No popularity.
- **Last.fm**: `album.getInfo` (has tracks in response), `artist.getTopTracks`, `artist.getTopAlbums`. Parses mbid from URL to extract artist/album names for the API calls.
- **iTunes / TheAudioDB**: skipped (no ID-based content lookups in free tier).
- **Return shape**: `ContentFetchResponse(provider_name, status, items: tuple[SearchResult, ...], latency_ms)`. Items carry `extras["track_position"]` (1-indexed) and `extras["duration_seconds"]` where available.
- **MusicBrainz featured artists** — `_extract_featured_artists(credits)` returns names from `artist-credit[1:]` into `extras["featured_artists"]` on recordings. Only populated when the MB recording has multi-artist credits.
- **SC resilience** — yt-dlp retries bumped to 2 (was 0), socket timeout 15s (was 10s). `get_artist_albums` retries once on empty entries before accepting empty. Diagnostic logging on username resolution failure and final set→album counts.

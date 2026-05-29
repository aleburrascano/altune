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
- **SoundCloud adapter is yt-dlp-based** [VERIFIED:Read@c:\Users\Alessandro\Desktop\altune\services\api\src\altune\adapters\outbound\discovery\soundcloud\adapter.py#L39-L47]; takes an async `extractor` callable (production wraps `yt_dlp.YoutubeDL.extract_info` in `asyncio.to_thread` because yt-dlp is sync). Tests inject a fake extractor returning the captured fixture.
- **yt-dlp options matter**: `extract_flat='in_playlist'` + `ignoreerrors=True` is non-negotiable. Without `extract_flat`, per-track 404s on removed/private tracks cascade-fail the whole scsearch. Captured during C4 fixture work.
- **`lookup_by_url` parses host-specific URL patterns** with `re.match`. Deezer parses `/track/<id>`; MB parses the MBID; Last.fm parses `/music/<Artist>/_/<Track>`; SC passes the URL straight to yt-dlp.
- **v1 supports tracks only across all adapters.** `search()` returns empty `()` when `ResultKind.TRACK not in kinds`. Per-provider artist/album/playlist endpoints are future-scope.
- **Cache TTL is not enforced by the adapter** — `setex` writes with the caller-supplied TTL; Redis enforces expiry. The adapter's `_VERSION_PREFIX = "v1"` is the manual invalidator [VERIFIED:Read@c:\Users\Alessandro\Desktop\altune\services\api\src\altune\adapters\outbound\discovery\cache\redis_cache.py#L24-L25].

## Known gotchas

- **MB User-Agent is non-optional** — without a registered UA with contact info, MB throttles to 1 req/s and may 503. Wiring constructs MB's `AsyncClient` with the UA header from `Settings.musicbrainz_user_agent`; wiring skips MB entirely if UA is unset (rather than spamming the public default).
- **Last.fm response shape returns `track` as dict (not list) when there's exactly one result.** The adapter normalizes this to a list before iterating [VERIFIED:Read@c:\Users\Alessandro\Desktop\altune\services\api\src\altune\adapters\outbound\discovery\lastfm\adapter.py#L105-L108].
- **SC fixture uses `t500x500` as the largest-by-width thumbnail**, NOT the `original` entry — the `original` thumbnail in yt-dlp output has no `width`, only `preference`. Tests assert `t500x500 in image_url`.
- **MusicBrainz now requests `inc=isrcs`** (ADR-0007 ranking-overhaul addendum) — `extras["isrc"]` is the first entry of the recording's `isrcs[]` array, or `None` when the recording has no ISRC. This revives the canonical cross-source ISRC merge with Deezer/iTunes; previously MB omitted ISRC and dedup fell back to JW only.
- **`# mypy: warn_unused_ignores = False`** is at the top of every adapter to silence the per-file mypy hook's noise about httpx / sqlalchemy stubs that the batch mypy resolves correctly.

## discover-music-v2 update

- **Album + artist search.** Deezer/iTunes/MusicBrainz/Last.fm each fan out to their album + artist endpoints concurrently inside one `search()` call (per-kind dispatch); SoundCloud stays tracks-only. Each carries an internal per-fetch HTTP-error mapping (e.g. `_DeezerHTTPError`).
- **Popularity** written to `extras["popularity"]`: Deezer track `rank` / artist `nb_fan`, Last.fm `listeners`, log-normalized. iTunes/MB carry none.
- **`DeezerSearchAdapter.resolve_artwork(kind, title, subtitle)`** implements the `ArtworkResolver` port — best-effort cover lookup (no-auth) used to back-fill art-less results (MB items, iTunes artists). Never raises.
- **Last.fm is NOT queried for artists** (resolved): its `artist.search` DB is crowd-scrobbled junk (track/beat titles posing as artists). Last.fm serves only TRACK + ALBUM here; artist entities come from iTunes / MusicBrainz / Deezer. The `_translate_one_artist` helper was removed.

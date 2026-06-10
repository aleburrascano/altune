# discovery HTTP router — bounded-context local rules

Six endpoints: `GET /v1/discovery/search`, `GET /v1/discovery/search-history`, `POST /v1/discovery/clicks`, plus catalog-browse routes `GET /v1/discovery/albums/{provider}/{external_id}/tracks`, `GET /v1/discovery/artists/{provider}/{external_id}/top-tracks`, `GET /v1/discovery/artists/{provider}/{external_id}/albums` (AC#14-20). Router is a thin shell — validate, build the per-request repo from `sessionmaker`, call the use case, serialize the output DTO.

## Key terms

- **`DiscoverySearchResponse`** — Pydantic v2 frozen model mirroring the wire contract. Holds `results: list[SearchResultDto]`, `providers: list[ProviderStatusDto]`, `partial: bool`, `cache: CacheDto`.
- **`DiscoveryClickRequest`** — request body for `POST /clicks`. Fields validated by Pydantic + explicit `ResultKind` / `Confidence` parsing at the handler boundary (raises HTTPException 422 on invalid).
- **`current_user_id` dependency** — same auth dep as catalog (ADR-0006 / `platform/auth.py`). Resolves the Supabase JWT to a `UserId`. 401 on missing/invalid token is the natural protection.
- **`save_history` query param** — `GET /search` accepts `save_history=false` (default true). When false, the use case skips `_persist_history`. Used by the mobile client for debounced as-you-type queries so intermediate partials don't bloat history chips.
- **Album limit** — `GET /artists/{provider}/{external_id}/albums` accepts `limit` up to 100 (`le=100`). Frontend sends 100 to get full discography depth from Deezer/MB.

## Patterns specific here

- **Per-request session via `request.app.state.sessionmaker`** [VERIFIED:Read@c:\Users\Alessandro\Desktop\altune\services\api\src\altune\adapters\inbound\http\discovery\router.py#L75-L80]. Falls back to `request.app.state.discovery_history_repo` (in-memory placeholder) when `sessionmaker` is unset (smoke / unit-test env).
- **`SearchMusic` is constructed fresh per request.** Trade-off documented in [application/discovery/CLAUDE.md](../../../../application/discovery/CLAUDE.md): circuit breakers reset every request in v1. Future refactor moves breakers to app.state.
- **`POST /v1/discovery/clicks` returns 202** (Accepted, no body) per AC#15. Persistence is awaited before the 202 — fire-and-forget *from the caller's perspective*, deterministic from the server's.
- **Validation matrix on `GET /search`**: `q` min_length=1 max_length=200 (422 on empty), `limit` ge=1 le=50 (422 outside), `kinds` parsed by `_parse_kinds` which raises HTTPException 422 on invalid values [VERIFIED:Read@c:\Users\Alessandro\Desktop\altune\services\api\src\altune\adapters\inbound\http\discovery\router.py#L40-L52].
- **No HTTPException for click validation either** — `kind` and `confidence` parsing raises HTTPException 422 explicitly when the enum cast fails.
- **No business logic in the router.** Translation between domain `SearchResult` and `SearchResultDto` happens in the handler body but is purely mechanical field-by-field copy. If the wire shape diverges from the domain, that mapping grows here.

## Known gotchas

- **`# mypy: ignore_errors = True`** at the top of [router.py](router.py) silences the per-file mypy hook's noise on fastapi / sqlalchemy / structlog imports. Batch mypy resolves them via `[[tool.mypy.overrides]]`.
- **Lifespan must run for `app.state` to be populated.** The e2e tests use `with TestClient(app) as client:` (context manager triggers lifespan); a plain `httpx.AsyncClient` against `ASGITransport(app=app)` does NOT trigger lifespan and will crash on `app.state.token_verifier` access.
- **Per-request import of `SqlAlchemySearchHistoryRepository` / `SqlAlchemySearchClickRepository`** — done inside the handler to avoid SQLAlchemy import overhead in environments that don't use persistence. Same pattern as `platform/wiring.py`'s lazy imports.
- **DTO field order is the wire contract.** The Pydantic models in [dto.py](dto.py) declare `kind, title, subtitle, image_url, confidence, sources, extras` in that order — match the spec §3.7 sample JSON exactly. Mobile's typed client mirrors this [VERIFIED:Read@c:\Users\Alessandro\Desktop\altune\apps\mobile\src\shared\api-client\discovery.ts#L22-L36].

## discover-music-v2 update

- **Kinds default to `{artist, album, track}`** (playlist removed; `_ALL_KINDS` derives from the enum).
- **Artwork back-fill is wired here:** the handler passes `app.state.discovery_artwork_resolver` (a `ChainedArtworkResolver`: Deezer → TheAudioDB, built in `platform/app.py`) into `SearchMusic(artwork_resolver=...)`. Falls back to the Deezer adapter from `discovery_providers` when app.state has no resolver (tests).
- **Popularity back-fill wired here** (discover-music-v3): the handler passes `app.state.discovery_popularity_resolver` (the Last.fm adapter, which has the api_key) into `SearchMusic(popularity_resolver=...)`; falls back to the lastfm provider in the list for tests.

## view-result-detail catalog browse (AC#14-20)

- **`GET /albums/{provider}/{external_id}/tracks`** — single-provider album tracklist fetch. Filters `discovery_providers` to those implementing `AlbumContentProvider` (has `get_album_tracks`). Returns `ContentFetchResponseDto` (items, provider, status, latency_ms). Unknown provider → ERROR status.
- **`GET /artists/{provider}/{external_id}/top-tracks`** / **`GET /artists/{provider}/{external_id}/albums`** — same pattern for artist content. Default limits 5 / 10.
- **`ContentFetchResponseDto`** — Pydantic v2 frozen model: `items: list[SearchResultDto]`, `provider: str`, `status: str`, `latency_ms: int`. Reuses `SearchResultDto` for item shape.
- **`_result_to_dto` helper** — factored from the search handler; converts domain `SearchResult` to wire `SearchResultDto`.

## discovery rework follow-up (2026-06-10)

- The search handler now also reads `discovery_track_title_source` (MB adapter, feeds the Genius hint retry) and `discovery_artwork_cache` from app.state and passes both into `SearchMusic` — in BOTH the sessionmaker and fallback constructions (the fallback branch previously omitted fanart/genius too; it no longer does).

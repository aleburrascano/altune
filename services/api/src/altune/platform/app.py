"""FastAPI application entrypoint.

Run with:
    uv run uvicorn altune.platform.app:app --reload
"""

from __future__ import annotations

from contextlib import asynccontextmanager
from typing import TYPE_CHECKING

from fastapi import FastAPI, Request
from fastapi.middleware.cors import CORSMiddleware

from altune import __version__
from altune.adapters.inbound.http.catalog.playlist_router import router as playlist_router
from altune.adapters.inbound.http.catalog.router import router as catalog_router
from altune.adapters.inbound.http.discovery.router import router as discovery_router
from altune.platform.config import Settings
from altune.platform.db import check_database, create_engine, create_sessionmaker
from altune.platform.logging import configure_logging, get_logger

if TYPE_CHECKING:
    from collections.abc import AsyncIterator

    from altune.application.discovery.ports import MbidResolver



def create_app(settings: Settings | None = None) -> FastAPI:
    """Application factory.

    AIDEV-NOTE: routers per bounded context are mounted here as they land.
    Keep this function declarative — wiring only, no business logic.

    Accepts an explicit Settings instance for tests (so each test can supply a
    different DATABASE_URL pointing at a testcontainers Postgres). When called
    with no argument it constructs Settings() from env / .env per the usual path.
    """
    cfg = settings or Settings()
    configure_logging(level=cfg.log_level, json=(cfg.env == "production"))
    log = get_logger(__name__)

    @asynccontextmanager
    async def lifespan(app: FastAPI) -> AsyncIterator[None]:
        """Initialize the DB on startup; dispose engine on shutdown.

        Per ADR-0003. If database_url is unset (e.g. unit-test usage of
        create_app), DB init is skipped and the /health endpoint reports
        db=not_configured. This lets the app boot for type-only / smoke tests.
        """
        # Settings on app.state for FastAPI deps (current_user_id) per ADR-0006.
        app.state.settings = cfg
        # TokenVerifier on app.state — current_user_id reads this. Construct
        # eagerly so misconfiguration fails at boot, not on first authenticated
        # request. The wiring lives in platform/wiring.py to keep this module's
        # import graph stable under the per-file mypy hook.
        from altune.platform.wiring import build_token_verifier

        app.state.token_verifier = build_token_verifier(cfg)
        if cfg.database_url is not None:
            engine = create_engine(cfg.database_url)
            app.state.engine = engine
            app.state.sessionmaker = create_sessionmaker(engine)
            log.info("db_initialized")
        # Redis (ADR-0007): one async client backing the whole discovery cache
        # layer (query cache + quality gates below). from_url does not connect
        # eagerly and every cache adapter degrades per-operation, so an
        # unavailable Redis never blocks boot or fails a search.
        if cfg.redis_url is not None:
            from redis.asyncio import Redis as _Redis

            from altune.adapters.outbound.discovery.cache.redis_cache import RedisQueryCache

            app.state.redis = _Redis.from_url(
                str(cfg.redis_url),
                decode_responses=True,
                socket_connect_timeout=1.0,
                socket_timeout=1.0,
            )
            app.state.discovery_cache = RedisQueryCache(redis=app.state.redis)
            log.info("redis_initialized")
        else:
            app.state.redis = None
            app.state.discovery_cache = None
        # Discovery wiring (per ADR-0007). httpx.AsyncClient per provider for
        # bulkhead isolation. Production SqlAlchemy repos land in slice 37;
        # v1 uses InMemory placeholders so the endpoint demos end-to-end.
        from altune.platform.wiring import (
            build_discovery_history_repo,
            build_discovery_providers,
        )

        discovery_clients, discovery_providers = build_discovery_providers(cfg)
        app.state.discovery_clients = discovery_clients
        app.state.discovery_providers = discovery_providers
        # Cover-art back-fill: chain Deezer (broad) then TheAudioDB (better art),
        # both of which implement ArtworkResolver.
        from altune.adapters.outbound.discovery.artwork import ChainedArtworkResolver
        from altune.application.discovery.ports import ArtworkResolver

        _art_chain = [
            p
            for p in discovery_providers
            if getattr(p, "name", None) in ("deezer", "theaudiodb")
            and isinstance(p, ArtworkResolver)
        ]
        app.state.discovery_artwork_resolver = ChainedArtworkResolver(resolvers=_art_chain)
        # Uniform popularity back-fill: the Last.fm adapter (has the api_key)
        # implements PopularityResolver via getInfo play counts.
        from altune.application.discovery.ports import PopularityResolver

        app.state.discovery_popularity_resolver = next(
            (
                p
                for p in discovery_providers
                if getattr(p, "name", None) == "lastfm" and isinstance(p, PopularityResolver)
            ),
            None,
        )
        app.state.discovery_history_repo = build_discovery_history_repo()

        # Fanart.tv: MBID-based artist images (provider-expansion Phase 1).
        if cfg.fanarttv_api_key:
            import httpx as _httpx

            from altune.adapters.outbound.discovery.fanarttv.adapter import (
                FanartTvArtworkResolver,
            )

            fanarttv_client = _httpx.AsyncClient(timeout=10.0)
            app.state.fanarttv_client = fanarttv_client
            app.state.discovery_fanart_resolver = FanartTvArtworkResolver(
                client=fanarttv_client, api_key=cfg.fanarttv_api_key
            )
        else:
            app.state.discovery_fanart_resolver = None

        # Genius: artist images for hip-hop/underground (fallback after Fanart.tv).
        if cfg.genius_access_token:
            import httpx as _httpx2

            from altune.adapters.outbound.discovery.genius.adapter import (
                GeniusArtworkResolver,
            )

            genius_client = _httpx2.AsyncClient(timeout=10.0)
            app.state.genius_client = genius_client
            app.state.discovery_genius_resolver = GeniusArtworkResolver(
                client=genius_client, access_token=cfg.genius_access_token
            )
        else:
            app.state.discovery_genius_resolver = None

        # Wikidata: cross-provider ID bridge (no auth, SPARQL endpoint).
        import httpx as _httpx3

        from altune.adapters.outbound.discovery.wikidata.adapter import (
            WikidataMbidResolver,
        )

        wikidata_client = _httpx3.AsyncClient(timeout=15.0)
        app.state.wikidata_client = wikidata_client
        app.state.discovery_wikidata_resolver = WikidataMbidResolver(client=wikidata_client)

        # Quality scorer + quality gates (discovery-foundation-v1).
        from altune.application.discovery.quality_scorer import compute_quality_score

        app.state.discovery_quality_scorer = compute_quality_score

        # MusicBrainz MBID resolver for cross-provider identity bridging.
        from altune.adapters.outbound.discovery.musicbrainz.adapter import (
            MusicBrainzMbidResolver,
        )

        mb_adapter = next(
            (p for p in discovery_providers if getattr(p, "name", None) == "musicbrainz"),
            None,
        )
        if mb_adapter is not None and hasattr(mb_adapter, "client"):
            mbid_resolver: MbidResolver = MusicBrainzMbidResolver(client=mb_adapter.client)
            if app.state.redis is not None:
                from altune.adapters.outbound.discovery.cache.mbid_cache import (
                    CachedMbidResolver,
                )

                mbid_resolver = CachedMbidResolver(inner=mbid_resolver, redis=app.state.redis)
            app.state.discovery_mbid_resolver = mbid_resolver
        else:
            app.state.discovery_mbid_resolver = None

        # Content validation + fetch success (quality gates, Redis-backed).
        redis_client = getattr(app.state, "redis", None)
        if redis_client is not None:
            from altune.adapters.outbound.discovery.cache.content_validation_cache import (
                RedisContentValidationCache,
            )
            from altune.adapters.outbound.discovery.cache.fetch_success_store import (
                RedisFetchSuccessStore,
            )

            app.state.discovery_content_validation_cache = RedisContentValidationCache(
                redis=redis_client
            )
            app.state.discovery_fetch_success_store = RedisFetchSuccessStore(redis=redis_client)
        else:
            app.state.discovery_content_validation_cache = None
            app.state.discovery_fetch_success_store = None
        # Audio acquisition wiring (acquire-track spec). Searcher + store on
        # app.state so the POST /v1/tracks background task can access them.
        if cfg.music_dir:
            from altune.adapters.outbound.audio.filesystem_store import FilesystemAudioStore
            from altune.adapters.outbound.audio.ytdlp_searcher import YtDlpAudioSearcher

            app.state.audio_searcher = YtDlpAudioSearcher(ffmpeg_location=cfg.ffmpeg_location)
            app.state.audio_store = FilesystemAudioStore(cfg.music_dir)
        log.info(
            "auth.startup_config_validated",
            verifier_mode="jwks" if cfg.supabase_jwt_jwks_url else "hs256",
            iss_expected=cfg.supabase_project_url,
            aud_expected=cfg.supabase_jwt_aud,
        )
        try:
            yield
        finally:
            stored_engine = getattr(app.state, "engine", None)
            if stored_engine is not None:
                await stored_engine.dispose()
                log.info("db_disposed")
            for client in getattr(app.state, "discovery_clients", ()):
                await client.aclose()
            stored_redis = getattr(app.state, "redis", None)
            if stored_redis is not None:
                await stored_redis.aclose()

    app = FastAPI(
        title="Altune API",
        version=__version__,
        description="Music manager backend.",
        lifespan=lifespan,
    )

    app.add_middleware(
        CORSMiddleware,
        allow_origins=cfg.cors_origins,
        allow_credentials=True,
        allow_methods=["*"],
        allow_headers=["*"],
    )

    app.include_router(catalog_router)
    app.include_router(playlist_router)
    app.include_router(discovery_router)

    # Register exception handlers (InvalidTokenError → 401, etc.).
    from altune.adapters.inbound.http.exception_handlers import (
        register_exception_handlers,
    )

    register_exception_handlers(app)

    # fastapi.* is in mypy ignore_missing_imports; the per-file hook flags this
    # decorator as untyped while the full-project mypy resolves it fine. Covering
    # both with the dual-code ignore so neither lane breaks.
    @app.get("/health", tags=["meta"])  # type: ignore[untyped-decorator, unused-ignore]
    async def health(request: Request) -> dict[str, str]:
        """Liveness + DB probe.

        Returns db=ok when SELECT 1 succeeds, db=down when it doesn't,
        db=not_configured when DATABASE_URL was unset at app start.
        """
        sessionmaker = getattr(request.app.state, "sessionmaker", None)
        if sessionmaker is None:
            return {"status": "ok", "version": __version__, "db": "not_configured"}
        db_ok = await check_database(sessionmaker)
        return {
            "status": "ok",
            "version": __version__,
            "db": "ok" if db_ok else "down",
        }

    log.info("app_initialized", env=cfg.env, version=__version__)
    return app


app = create_app()

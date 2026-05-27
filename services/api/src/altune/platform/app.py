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
from altune.adapters.inbound.http.catalog.router import router as catalog_router
from altune.platform.config import Settings
from altune.platform.db import check_database, create_engine, create_sessionmaker
from altune.platform.logging import configure_logging, get_logger

if TYPE_CHECKING:
    from collections.abc import AsyncIterator


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
        # Settings on app.state for FastAPI deps (current_user_id) per ADR-0004.
        app.state.settings = cfg
        if cfg.database_url is not None:
            engine = create_engine(cfg.database_url)
            app.state.engine = engine
            app.state.sessionmaker = create_sessionmaker(engine)
            log.info("db_initialized")
        try:
            yield
        finally:
            stored_engine = getattr(app.state, "engine", None)
            if stored_engine is not None:
                await stored_engine.dispose()
                log.info("db_disposed")

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

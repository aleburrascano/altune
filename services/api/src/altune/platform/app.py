"""FastAPI application entrypoint.

Run with:
    uv run uvicorn altune.platform.app:app --reload
"""

from __future__ import annotations

from typing import Final

from fastapi import FastAPI
from fastapi.middleware.cors import CORSMiddleware

from altune import __version__
from altune.platform.config import Settings
from altune.platform.logging import configure_logging, get_logger

settings: Final = Settings()
configure_logging(level=settings.log_level, json=(settings.env == "production"))
log = get_logger(__name__)


def create_app() -> FastAPI:
    """Application factory.

    AIDEV-NOTE: routers per bounded context are mounted here as they land.
    Keep this function declarative — wiring only, no business logic.
    """
    app = FastAPI(
        title="Altune API",
        version=__version__,
        description="Music manager backend.",
    )

    app.add_middleware(
        CORSMiddleware,
        allow_origins=settings.cors_origins,
        allow_credentials=True,
        allow_methods=["*"],
        allow_headers=["*"],
    )

    @app.get("/health", tags=["meta"])
    async def health() -> dict[str, str]:
        """Liveness probe. Replaced with a real health-check once dependencies exist."""
        return {"status": "ok", "version": __version__}

    log.info("app_initialized", env=settings.env, version=__version__)
    return app


app = create_app()

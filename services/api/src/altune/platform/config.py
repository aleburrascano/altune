"""Typed configuration via pydantic-settings.

Env vars override defaults. .env loaded in development.
"""

from __future__ import annotations

from pydantic import Field
from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    """Application-wide configuration."""

    model_config = SettingsConfigDict(
        env_file=".env",
        env_file_encoding="utf-8",
        extra="ignore",
        frozen=True,
    )

    env: str = Field(default="development", description="development | staging | production")
    log_level: str = Field(default="INFO")

    host: str = Field(default="0.0.0.0")  # noqa: S104  # binding to all interfaces is intentional in dev
    port: int = Field(default=8000)

    cors_origins: list[str] = Field(
        default_factory=lambda: ["http://localhost:8081", "http://localhost:19006"],
        description="Comma-separated list (parsed by pydantic-settings).",
    )

    # AIDEV-NOTE: persistence + auth + sentry settings land here when the corresponding ADRs ship.

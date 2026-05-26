"""Typed configuration via pydantic-settings.

Env vars override defaults. .env loaded in development.
"""

from __future__ import annotations

from typing import Literal
from uuid import UUID  # noqa: TC003  # pydantic needs runtime access for UUID field validation

from pydantic import Field
from pydantic_settings import BaseSettings, SettingsConfigDict

Env = Literal["development", "test", "production"]


class Settings(BaseSettings):
    """Application-wide configuration."""

    model_config = SettingsConfigDict(
        env_file=".env",
        env_file_encoding="utf-8",
        extra="ignore",
        frozen=True,
    )

    env: Env = Field(default="development", description="development | test | production")
    log_level: str = Field(default="INFO")

    host: str = Field(default="0.0.0.0")  # noqa: S104  # binding to all interfaces is intentional in dev
    port: int = Field(default=8000)

    cors_origins: list[str] = Field(
        default_factory=lambda: ["http://localhost:8081", "http://localhost:19006"],
        description="Comma-separated list (parsed by pydantic-settings).",
    )

    # Persistence — per ADR-0003. Optional at field level so unit tests can
    # construct Settings without provisioning a DB; consumers (db.py engine
    # factory) validate presence at the point of use.
    database_url: str | None = Field(
        default=None,
        description="postgresql+asyncpg://user:pass@host:port/dbname",
    )

    # Multi-tenancy — per ADR-0004. Single hardcoded user for v1 dev/test.
    # The prod-startup guard refuses to construct Settings when env=production
    # AND this is set; that's enforced in a model_validator in a follow-on slice.
    hardcoded_user_id: UUID | None = Field(
        default=None,
        description="UUID for the dev single-user mode. Must be unset in production.",
    )

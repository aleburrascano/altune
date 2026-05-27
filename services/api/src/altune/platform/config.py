"""Typed configuration via pydantic-settings.

Env vars override defaults. .env loaded in development.
"""

from __future__ import annotations

from typing import Literal, Self

from pydantic import Field, model_validator
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

    # Supabase Auth — per ADR-0006 (auth-integration spec, AC#13). The verifier
    # at adapters/outbound/auth/supabase_jwt_verifier.py consumes these. Exactly
    # one of supabase_jwt_secret (HS256) or supabase_jwt_jwks_url (JWKS) must be
    # set at runtime; the XOR validator below enforces this independent of env.
    supabase_project_url: str | None = Field(
        default=None,
        description="Supabase project URL, e.g. https://<ref>.supabase.co. Used as the JWT iss base.",
    )
    supabase_jwt_aud: str = Field(
        default="authenticated",
        description="Expected aud claim on the JWT. Supabase default is 'authenticated'.",
    )
    supabase_jwt_secret: str | None = Field(
        default=None,
        description="HS256 shared secret. Mutually exclusive with supabase_jwt_jwks_url.",
    )
    supabase_jwt_jwks_url: str | None = Field(
        default=None,
        description="JWKS endpoint URL, e.g. https://<ref>.supabase.co/auth/v1/keys. "
        "Mutually exclusive with supabase_jwt_secret.",
    )

    @model_validator(mode="after")
    def _validate_supabase_jwt_mode_xor(self) -> Self:
        # AIDEV-NOTE: ADR-0006 (auth-integration, AC#13). Exactly one verification
        # mode must be configured — both set is ambiguous (which key wins?); neither
        # set means the verifier cannot be constructed. Enforced unconditionally
        # (independent of env) so misconfiguration fails fast at startup.
        has_secret = self.supabase_jwt_secret is not None
        has_jwks = self.supabase_jwt_jwks_url is not None
        if has_secret and has_jwks:
            raise ValueError(
                "SUPABASE_JWT_SECRET and SUPABASE_JWT_JWKS_URL are mutually exclusive; "
                "set exactly one (per ADR-0006)"
            )
        if not has_secret and not has_jwks:
            raise ValueError(
                "Either SUPABASE_JWT_SECRET or SUPABASE_JWT_JWKS_URL must be set (per ADR-0006)"
            )
        return self

"""Typed configuration via pydantic-settings.

Env vars override defaults. .env loaded in development.
"""

from __future__ import annotations

from typing import Annotated, Any, Literal, Self

from pydantic import Field, RedisDsn, SecretStr, field_validator, model_validator
from pydantic_settings import BaseSettings, NoDecode, SettingsConfigDict

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

    # AIDEV-NOTE: NoDecode tells pydantic-settings NOT to JSON-decode the env
    # value before passing it to the field validator. Without this, a bare
    # comma-separated string in .env crashes the env source's prepare step
    # with JSONDecodeError BEFORE the @field_validator below ever runs. With
    # NoDecode, the raw string reaches the validator and we split it.
    cors_origins: Annotated[list[str], NoDecode] = Field(
        default_factory=lambda: ["http://localhost:8081", "http://localhost:19006"],
        description="Comma-separated list of origin URLs (parsed by the validator below).",
    )

    @field_validator("cors_origins", mode="before")
    @classmethod
    def _split_cors_origins(cls, value: Any) -> Any:
        # Accept: real list (default / explicit list), JSON-encoded list
        # ([\"a\",\"b\"]), or a comma-separated string.
        if isinstance(value, str):
            stripped = value.strip()
            if stripped.startswith("["):
                import json

                return json.loads(stripped)
            return [item.strip() for item in stripped.split(",") if item.strip()]
        return value

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
        description="JWKS endpoint URL, e.g. "
        "https://<ref>.supabase.co/auth/v1/.well-known/jwks.json. "
        "Mutually exclusive with supabase_jwt_secret.",
    )

    # Discovery — per ADR-0007 (discover-music-v1). Provider credentials +
    # cache infra. SoundCloud is via yt-dlp (no env vars) per the ADR's
    # 2026-05-27 strategy revision (Artist Pro requirement blocked the
    # official Developer API path).
    #
    # All fields are Optional with None default — matches the database_url
    # and supabase_* precedent. Unit tests can construct Settings without
    # provisioning the discovery stack; consumers (cache adapter, provider
    # adapters) fail fast at startup if their required setting is None.
    redis_url: RedisDsn | None = Field(
        default=None,
        description="Redis URL for the discovery query cache. "
        "Example: redis://localhost:6379/0. Per ADR-0007.",
    )
    musicbrainz_user_agent: str | None = Field(
        default=None,
        description="MusicBrainz API User-Agent header. When set, MUST include a contact "
        "form URL or email per https://musicbrainz.org/doc/MusicBrainz_API/Rate_Limiting. "
        "Example: 'altune/0.1 ( mailto:dev@altune.test )'.",
    )
    music_dir: str | None = Field(
        default=None,
        description="Base path for audio file storage, e.g. /mnt/oci-music. "
        "Required at runtime for audio acquisition; optional so unit tests "
        "can construct Settings without provisioning storage.",
    )
    ffmpeg_location: str | None = Field(
        default=None,
        description="Path to ffmpeg binary directory. If unset, yt-dlp uses system PATH.",
    )

    lastfm_api_key: SecretStr | None = Field(
        default=None,
        description="Last.fm API key for the discovery adapter. Obtain via "
        "https://www.last.fm/api/account/create.",
    )
    fanarttv_api_key: str | None = Field(
        default=None,
        description="Fanart.tv API key for MBID-based artist images. "
        "Register at https://fanart.tv/get-an-api-key/.",
    )
    genius_access_token: str | None = Field(
        default=None,
        description="Genius API access token for artist images + metadata. "
        "Register at https://genius.com/api-clients.",
    )

    @field_validator("musicbrainz_user_agent")
    @classmethod
    def _validate_musicbrainz_user_agent_has_contact(cls, value: str | None) -> str | None:
        # AIDEV-NOTE: MB throttles to 1 req/s for User-Agents without contact
        # info; ~50 req/s for registered ones. The contact form OR email is
        # the only path to the higher rate budget; failing fast here keeps
        # the rate-limit-pressure-in-production failure mode visible at startup.
        # Validator skipped when value is None (consumer validates presence).
        if value is None:
            return value
        if "@" not in value and "http" not in value.lower():
            raise ValueError(
                "MUSICBRAINZ_USER_AGENT must contain a contact form URL or email "
                "(e.g. 'altune/0.1 ( mailto:dev@altune.test )') — per MB's "
                "rate-limit policy."
            )
        return value

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

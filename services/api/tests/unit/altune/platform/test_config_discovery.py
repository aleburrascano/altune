"""Settings parses the discover-music-v1 fields per ADR-0007.

Discovery-specific fields: redis_url, lastfm_api_key, musicbrainz_user_agent.
SoundCloud is yt-dlp-based per ADR-0007's strategy revision; no SC env vars.
"""

from __future__ import annotations

import pytest
from pydantic import SecretStr, ValidationError

from altune.platform.config import Settings


def _clean_with_baseline(monkeypatch: pytest.MonkeyPatch) -> None:
    """Drop env vars this module cares about; set minimum Supabase baseline.

    Per ADR-0006, Settings requires exactly one Supabase JWT mode set. Tests
    in this module are not about JWT mode — they set a fixture JWKS URL so
    construction succeeds without repeating boilerplate.
    """
    for var in (
        "ENV",
        "REDIS_URL",
        "MUSICBRAINZ_USER_AGENT",
        "LASTFM_API_KEY",
        "SUPABASE_PROJECT_URL",
        "SUPABASE_JWT_AUD",
        "SUPABASE_JWT_SECRET",
        "SUPABASE_JWT_JWKS_URL",
    ):
        monkeypatch.delenv(var, raising=False)
    monkeypatch.setenv("SUPABASE_JWT_JWKS_URL", "https://fixture.supabase.co/auth/v1/keys")


@pytest.mark.unit
def test_settings_accepts_well_formed_redis_url(monkeypatch: pytest.MonkeyPatch) -> None:
    _clean_with_baseline(monkeypatch)
    monkeypatch.setenv("REDIS_URL", "redis://localhost:6379/0")
    monkeypatch.setenv("MUSICBRAINZ_USER_AGENT", "altune/0.1 ( mailto:dev@altune.test )")
    monkeypatch.setenv("LASTFM_API_KEY", "fixture-key")
    s = Settings(_env_file=None)  # type: ignore[call-arg]
    assert str(s.redis_url) == "redis://localhost:6379/0"


@pytest.mark.unit
@pytest.mark.parametrize(
    "malformed",
    [
        "not-a-url",
        "http://localhost:6379",  # wrong scheme
        "",  # empty
        # NOTE: `redis://` (scheme-only) is accepted by pydantic's RedisDsn —
        # known gap; cache adapter surfaces the connection error at runtime.
    ],
)
def test_settings_rejects_malformed_redis_url(
    monkeypatch: pytest.MonkeyPatch, malformed: str
) -> None:
    _clean_with_baseline(monkeypatch)
    monkeypatch.setenv("REDIS_URL", malformed)
    monkeypatch.setenv("MUSICBRAINZ_USER_AGENT", "altune/0.1 ( mailto:dev@altune.test )")
    monkeypatch.setenv("LASTFM_API_KEY", "fixture-key")
    with pytest.raises(ValidationError):
        Settings(_env_file=None)  # type: ignore[call-arg]


@pytest.mark.unit
def test_settings_lastfm_api_key_is_secret_str_and_round_trips(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    _clean_with_baseline(monkeypatch)
    monkeypatch.setenv("REDIS_URL", "redis://localhost:6379/0")
    monkeypatch.setenv("MUSICBRAINZ_USER_AGENT", "altune/0.1 ( mailto:dev@altune.test )")
    monkeypatch.setenv("LASTFM_API_KEY", "real-api-key-value")
    s = Settings(_env_file=None)  # type: ignore[call-arg]
    assert isinstance(s.lastfm_api_key, SecretStr)
    assert s.lastfm_api_key.get_secret_value() == "real-api-key-value"
    # Repr never leaks the value (SecretStr's protection).
    assert "real-api-key-value" not in repr(s)


@pytest.mark.unit
def test_settings_accepts_musicbrainz_user_agent_with_mailto(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    _clean_with_baseline(monkeypatch)
    monkeypatch.setenv("REDIS_URL", "redis://localhost:6379/0")
    monkeypatch.setenv("MUSICBRAINZ_USER_AGENT", "altune/0.1 ( mailto:dev@altune.test )")
    monkeypatch.setenv("LASTFM_API_KEY", "fixture-key")
    s = Settings(_env_file=None)  # type: ignore[call-arg]
    assert s.musicbrainz_user_agent is not None
    assert "@" in s.musicbrainz_user_agent


@pytest.mark.unit
def test_settings_accepts_musicbrainz_user_agent_with_contact_url(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    _clean_with_baseline(monkeypatch)
    monkeypatch.setenv("REDIS_URL", "redis://localhost:6379/0")
    monkeypatch.setenv(
        "MUSICBRAINZ_USER_AGENT", "altune/0.1 ( https://altune.example/contact )"
    )
    monkeypatch.setenv("LASTFM_API_KEY", "fixture-key")
    s = Settings(_env_file=None)  # type: ignore[call-arg]
    assert s.musicbrainz_user_agent is not None
    assert "https://" in s.musicbrainz_user_agent


@pytest.mark.unit
def test_settings_rejects_musicbrainz_user_agent_without_contact_form_or_email(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    _clean_with_baseline(monkeypatch)
    monkeypatch.setenv("REDIS_URL", "redis://localhost:6379/0")
    monkeypatch.setenv("MUSICBRAINZ_USER_AGENT", "altune/0.1")  # no contact
    monkeypatch.setenv("LASTFM_API_KEY", "fixture-key")
    with pytest.raises(ValidationError):
        Settings(_env_file=None)  # type: ignore[call-arg]


@pytest.mark.unit
def test_settings_discovery_fields_default_to_none_when_env_unset(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    # Mirrors the precedent in test_settings_defaults_when_env_unset for
    # database_url. Consumers (cache adapter, provider adapters) fail fast
    # at startup when their required setting is None.
    _clean_with_baseline(monkeypatch)
    s = Settings(_env_file=None)  # type: ignore[call-arg]
    assert s.redis_url is None
    assert s.musicbrainz_user_agent is None
    assert s.lastfm_api_key is None

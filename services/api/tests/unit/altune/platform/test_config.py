"""Settings parses persistence, env, and hardcoded user id fields.

Companion ADRs: 0003 (persistence stack), 0004 (multi-tenancy posture).
The prod-startup guard is exercised in test_startup_guard.py (slice 6).
"""

from __future__ import annotations

from uuid import UUID

import pytest
from pydantic import ValidationError

from altune.platform.config import Settings


def _clean(monkeypatch: pytest.MonkeyPatch) -> None:
    """Drop env vars this module cares about; set the minimum Supabase config baseline.

    Per ADR-0006 (auth-integration spec, AC#13), Settings requires exactly one of
    SUPABASE_JWT_{SECRET,JWKS_URL}. Tests in this module are not about JWT mode —
    they set a fixture JWKS URL here so construction succeeds without repeating
    boilerplate in every test.
    """
    for var in (
        "DATABASE_URL",
        "ENV",
        "HARDCODED_USER_ID",
        "SUPABASE_PROJECT_URL",
        "SUPABASE_JWT_AUD",
        "SUPABASE_JWT_SECRET",
        "SUPABASE_JWT_JWKS_URL",
    ):
        monkeypatch.delenv(var, raising=False)
    monkeypatch.setenv("SUPABASE_JWT_JWKS_URL", "https://fixture.supabase.co/auth/v1/keys")


@pytest.mark.unit
def test_settings_defaults_when_env_unset(monkeypatch: pytest.MonkeyPatch) -> None:
    _clean(monkeypatch)
    s = Settings(_env_file=None)  # type: ignore[call-arg]
    assert s.database_url is None
    assert s.env == "development"
    assert s.hardcoded_user_id is None


@pytest.mark.unit
def test_settings_loads_database_url(monkeypatch: pytest.MonkeyPatch) -> None:
    _clean(monkeypatch)
    monkeypatch.setenv("DATABASE_URL", "postgresql+asyncpg://altune:dev@localhost/altune")
    s = Settings(_env_file=None)  # type: ignore[call-arg]
    assert s.database_url == "postgresql+asyncpg://altune:dev@localhost/altune"


@pytest.mark.unit
@pytest.mark.parametrize("value", ["development", "test", "production"])
def test_settings_env_accepts_allowed_values(monkeypatch: pytest.MonkeyPatch, value: str) -> None:
    _clean(monkeypatch)
    monkeypatch.setenv("ENV", value)
    s = Settings(_env_file=None)  # type: ignore[call-arg]
    assert s.env == value


@pytest.mark.unit
def test_settings_env_rejects_unknown_value(monkeypatch: pytest.MonkeyPatch) -> None:
    _clean(monkeypatch)
    monkeypatch.setenv("ENV", "staging")  # not in the Literal
    with pytest.raises(ValidationError):
        Settings(_env_file=None)  # type: ignore[call-arg]


@pytest.mark.unit
def test_settings_hardcoded_user_id_parses_uuid(monkeypatch: pytest.MonkeyPatch) -> None:
    _clean(monkeypatch)
    monkeypatch.setenv("HARDCODED_USER_ID", "00000000-0000-0000-0000-000000000001")
    s = Settings(_env_file=None)  # type: ignore[call-arg]
    assert s.hardcoded_user_id == UUID("00000000-0000-0000-0000-000000000001")


@pytest.mark.unit
def test_settings_hardcoded_user_id_rejects_garbage(monkeypatch: pytest.MonkeyPatch) -> None:
    _clean(monkeypatch)
    monkeypatch.setenv("HARDCODED_USER_ID", "not-a-uuid")
    with pytest.raises(ValidationError):
        Settings(_env_file=None)  # type: ignore[call-arg]

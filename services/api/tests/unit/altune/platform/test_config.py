"""Settings parses persistence + env fields under the post-ADR-0006 posture.

Companion ADRs: 0003 (persistence stack), 0006 (Supabase Auth, which
supersedes 0004's hardcoded-user-id posture). Settings now requires
exactly one of SUPABASE_JWT_{SECRET,JWKS_URL}; the XOR validator is
exercised in test_settings_supabase_xor.py.
"""

from __future__ import annotations

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

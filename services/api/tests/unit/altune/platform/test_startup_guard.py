"""Prod-startup guard refuses Settings when ENV=production + HARDCODED_USER_ID set.

Per ADR-0004 — prevents the hardcoded dev user id from silently leaking into a
production deploy. The guard is a model_validator on Settings so failure happens
at config construction, before the FastAPI app even starts.
"""

from __future__ import annotations

import pytest
from pydantic import ValidationError

from altune.platform.config import Settings

_DEV_UUID = "00000000-0000-0000-0000-000000000001"


def _clean(monkeypatch: pytest.MonkeyPatch) -> None:
    """Drop env vars; set a minimum Supabase config baseline.

    Per ADR-0006 (auth-integration spec, AC#13), Settings requires one of
    SUPABASE_JWT_{SECRET,JWKS_URL}. These tests focus on the prod-startup guard,
    not the JWT mode — set a fixture JWKS URL so Settings construction succeeds.
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
def test_prod_with_hardcoded_user_id_refuses_construction(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    _clean(monkeypatch)
    monkeypatch.setenv("ENV", "production")
    monkeypatch.setenv("HARDCODED_USER_ID", _DEV_UUID)
    with pytest.raises(ValidationError, match=r"HARDCODED_USER_ID"):
        Settings(_env_file=None)  # type: ignore[call-arg]


@pytest.mark.unit
def test_prod_without_hardcoded_user_id_constructs(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    _clean(monkeypatch)
    monkeypatch.setenv("ENV", "production")
    s = Settings(_env_file=None)  # type: ignore[call-arg]
    assert s.env == "production"
    assert s.hardcoded_user_id is None


@pytest.mark.unit
@pytest.mark.parametrize("env", ["development", "test"])
def test_non_prod_with_hardcoded_user_id_constructs(
    monkeypatch: pytest.MonkeyPatch, env: str
) -> None:
    _clean(monkeypatch)
    monkeypatch.setenv("ENV", env)
    monkeypatch.setenv("HARDCODED_USER_ID", _DEV_UUID)
    s = Settings(_env_file=None)  # type: ignore[call-arg]
    assert s.env == env
    assert s.hardcoded_user_id is not None

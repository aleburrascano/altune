# mypy: warn_unused_ignores = False
"""Settings XOR validator on the Supabase JWT verification mode.

Per the forthcoming ADR-0006 (auth-integration spec, AC#13): exactly one of
SUPABASE_JWT_SECRET (HS256) or SUPABASE_JWT_JWKS_URL (JWKS) must be configured
at runtime. Both set → ValidationError. Neither set → ValidationError. The
constraint is independent of ENV (development / test / production all enforce).

This file covers the rejection branches (RED phase). Positive-path tests
(boots-with-secret-only, boots-with-jwks-url-only) land in a same-slice
follow-on commit once the fields exist on Settings.

The `warn_unused_ignores = False` pragma above is a defensive measure: the
post-write-langcheck hook runs mypy single-file from the repo root, which
non-deterministically resolves `altune.platform.config` depending on the
mypy-cache state. The targeted ignore below covers the "import not found"
case; the pragma keeps the ignore quiet when mypy *does* resolve.
"""

from __future__ import annotations

import pytest
from pydantic import ValidationError

from altune.platform.config import Settings  # type: ignore[import-not-found]

_JWKS_URL = "https://fixture.supabase.co/auth/v1/keys"
_SECRET = "fixture-shared-secret"  # fixture string, not a real secret


def _clean(monkeypatch: pytest.MonkeyPatch) -> None:
    """Drop every env var this module cares about so .env / parent env can't bleed in."""
    for var in (
        "DATABASE_URL",
        "ENV",
        "SUPABASE_PROJECT_URL",
        "SUPABASE_JWT_AUD",
        "SUPABASE_JWT_SECRET",
        "SUPABASE_JWT_JWKS_URL",
    ):
        monkeypatch.delenv(var, raising=False)


@pytest.mark.unit
def test_settings_rejects_when_both_supabase_jwt_secret_and_jwks_url_set(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    _clean(monkeypatch)
    monkeypatch.setenv("SUPABASE_JWT_SECRET", _SECRET)
    monkeypatch.setenv("SUPABASE_JWT_JWKS_URL", _JWKS_URL)
    with pytest.raises(ValidationError, match=r"SUPABASE_JWT"):
        Settings(_env_file=None)  # type: ignore[call-arg]


@pytest.mark.unit
def test_settings_rejects_when_neither_secret_nor_jwks_url_set(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    _clean(monkeypatch)
    with pytest.raises(ValidationError, match=r"SUPABASE_JWT"):
        Settings(_env_file=None)  # type: ignore[call-arg]


@pytest.mark.unit
def test_settings_boots_with_secret_only(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    _clean(monkeypatch)
    monkeypatch.setenv("SUPABASE_JWT_SECRET", _SECRET)
    s = Settings(_env_file=None)  # type: ignore[call-arg]
    assert s.supabase_jwt_secret == _SECRET
    assert s.supabase_jwt_jwks_url is None


@pytest.mark.unit
def test_settings_boots_with_jwks_url_only(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    _clean(monkeypatch)
    monkeypatch.setenv("SUPABASE_JWT_JWKS_URL", _JWKS_URL)
    s = Settings(_env_file=None)  # type: ignore[call-arg]
    assert s.supabase_jwt_jwks_url == _JWKS_URL
    assert s.supabase_jwt_secret is None

"""Settings parses persistence + env fields under the post-ADR-0006 posture.

Companion ADRs: 0003 (persistence stack), 0006 (Supabase Auth, which
supersedes 0004's hardcoded-user-id posture). Settings now requires
exactly one of SUPABASE_JWT_{SECRET,JWKS_URL}; the XOR validator is
exercised in test_settings_supabase_xor.py.
"""

from __future__ import annotations

from typing import TYPE_CHECKING

import pytest
from pydantic import ValidationError

if TYPE_CHECKING:
    from pathlib import Path

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
        "YTDLP_COOKIE_FILE",
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


@pytest.mark.unit
def test_settings_ytdlp_cookie_file_defaults_to_none(monkeypatch: pytest.MonkeyPatch) -> None:
    _clean(monkeypatch)
    s = Settings(_env_file=None)  # type: ignore[call-arg]
    assert s.ytdlp_cookie_file is None


@pytest.mark.unit
def test_settings_ytdlp_cookie_file_accepts_existing_file(
    monkeypatch: pytest.MonkeyPatch,
    tmp_path: Path,
) -> None:
    _clean(monkeypatch)
    cookie_file = tmp_path / "cookies.txt"
    cookie_file.write_text("# Netscape HTTP Cookie File\n")
    monkeypatch.setenv("YTDLP_COOKIE_FILE", str(cookie_file))
    s = Settings(_env_file=None)  # type: ignore[call-arg]
    assert s.ytdlp_cookie_file == str(cookie_file)


@pytest.mark.unit
def test_settings_ytdlp_cookie_file_rejects_nonexistent(monkeypatch: pytest.MonkeyPatch) -> None:
    _clean(monkeypatch)
    monkeypatch.setenv("YTDLP_COOKIE_FILE", "/nonexistent/cookies.txt")
    with pytest.raises(ValidationError, match="does not exist"):
        Settings(_env_file=None)  # type: ignore[call-arg]

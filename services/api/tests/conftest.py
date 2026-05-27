"""Shared pytest fixtures.

Per-test in-memory adapter implementations live here as features land.
"""

from __future__ import annotations

import os

import pytest


def pytest_configure(config: pytest.Config) -> None:
    _ = config  # unused; signature matches pytest's pytest_configure hook
    """Provide a default SUPABASE_JWT_JWKS_URL so Settings() satisfies the
    ADR-0006 XOR validator in tests that don't explicitly configure it. Tests
    that DO care about the XOR's behavior set/clear the env vars themselves
    via monkeypatch.
    """
    os.environ.setdefault(
        "SUPABASE_JWT_JWKS_URL",
        "https://fixture.supabase.co/auth/v1/keys",
    )


@pytest.fixture
def anyio_backend() -> str:
    """Pin async backend for pytest-asyncio interop with httpx async testing."""
    return "asyncio"

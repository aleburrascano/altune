"""Shared pytest fixtures.

Per-test in-memory adapter implementations live here as features land.
"""

from __future__ import annotations

import pytest


@pytest.fixture
def anyio_backend() -> str:
    """Pin async backend for pytest-asyncio interop with httpx async testing."""
    return "asyncio"

# mypy: warn_unused_ignores = False
"""current_user_id consumer tests (Slice 4, AC#10).

Uses a duck-typed Request stub that exposes only the attributes the dependency
reads: `.headers.get(name)` and `.app.state.token_verifier`. The verifier is
the InMemoryTokenVerifier stub from Slice 2, configured per scenario.
"""

from __future__ import annotations

from dataclasses import dataclass
from typing import Any
from uuid import UUID

import pytest
from tests._doubles.in_memory_token_verifier import (  # type: ignore[import-not-found]
    InMemoryTokenVerifier,
)

from altune.application.auth.exceptions import (  # type: ignore[import-not-found]
    InvalidTokenError,
    TokenRejectReason,
)
from altune.domain.shared.user_id import UserId  # type: ignore[import-not-found]
from altune.platform.auth import current_user_id  # type: ignore[import-not-found]

_BEARER = "test-bearer-token"
_USER_ID = UserId(UUID("00000000-0000-0000-0000-000000000042"))


@dataclass
class _Headers:
    values: dict[str, str]

    def get(self, name: str) -> str | None:
        # Starlette/FastAPI Headers are case-insensitive; mimic that.
        for key, value in self.values.items():
            if key.lower() == name.lower():
                return value
        return None


@dataclass
class _AppState:
    token_verifier: Any


@dataclass
class _App:
    state: _AppState


@dataclass
class _Request:
    headers: _Headers
    app: _App


def _make_request(authorization: str | None, verifier: Any) -> _Request:
    headers = _Headers({"authorization": authorization} if authorization is not None else {})
    return _Request(headers=headers, app=_App(state=_AppState(token_verifier=verifier)))


@pytest.mark.unit
@pytest.mark.asyncio
async def test_current_user_id_returns_user_id_when_verifier_succeeds() -> None:
    verifier = InMemoryTokenVerifier(mapping={_BEARER: _USER_ID})
    request = _make_request(f"Bearer {_BEARER}", verifier)

    result = await current_user_id(request)  # type: ignore[arg-type]

    assert result == _USER_ID


@pytest.mark.unit
@pytest.mark.asyncio
async def test_current_user_id_raises_invalid_token_error_when_authorization_header_missing() -> (
    None
):
    verifier = InMemoryTokenVerifier(mapping={_BEARER: _USER_ID})
    request = _make_request(None, verifier)

    with pytest.raises(InvalidTokenError) as exc_info:
        await current_user_id(request)  # type: ignore[arg-type]

    assert exc_info.value.reason == TokenRejectReason.MISSING


@pytest.mark.unit
@pytest.mark.asyncio
async def test_current_user_id_raises_invalid_token_error_when_header_malformed() -> None:
    verifier = InMemoryTokenVerifier(mapping={_BEARER: _USER_ID})
    request = _make_request("NotBearer something", verifier)

    with pytest.raises(InvalidTokenError) as exc_info:
        await current_user_id(request)  # type: ignore[arg-type]

    assert exc_info.value.reason == TokenRejectReason.MALFORMED


@pytest.mark.unit
@pytest.mark.asyncio
async def test_current_user_id_raises_invalid_token_error_when_verifier_rejects() -> None:
    verifier = InMemoryTokenVerifier(raise_reason=TokenRejectReason.EXPIRED)
    request = _make_request(f"Bearer {_BEARER}", verifier)

    with pytest.raises(InvalidTokenError) as exc_info:
        await current_user_id(request)  # type: ignore[arg-type]

    assert exc_info.value.reason == TokenRejectReason.EXPIRED


@pytest.mark.unit
@pytest.mark.asyncio
async def test_current_user_id_strips_bearer_prefix_case_insensitive() -> None:
    verifier = InMemoryTokenVerifier(mapping={_BEARER: _USER_ID})
    request = _make_request(f"BeArEr {_BEARER}", verifier)

    result = await current_user_id(request)  # type: ignore[arg-type]

    assert result == _USER_ID

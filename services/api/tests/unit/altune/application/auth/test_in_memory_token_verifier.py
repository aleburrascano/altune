# mypy: warn_unused_ignores = False
"""InMemoryTokenVerifier — behavior tests.

The stub is the foundation for Slice 4's consumer unit tests (current_user_id)
and serves as the contract the SupabaseJwtVerifier adapter shadows in Slice 3a.
"""

from __future__ import annotations

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

_BEARER = "test-bearer-token"
_USER_ID = UserId(UUID("00000000-0000-0000-0000-000000000042"))


@pytest.mark.unit
@pytest.mark.asyncio
async def test_in_memory_verifier_returns_configured_user_id_for_known_bearer() -> None:
    verifier = InMemoryTokenVerifier(mapping={_BEARER: _USER_ID})

    result = await verifier.verify(_BEARER)

    assert result == _USER_ID


@pytest.mark.unit
@pytest.mark.asyncio
async def test_in_memory_verifier_raises_invalid_token_error_for_unknown_bearer() -> None:
    verifier = InMemoryTokenVerifier()

    with pytest.raises(InvalidTokenError) as exc_info:
        await verifier.verify("unknown-bearer")

    assert exc_info.value.reason == TokenRejectReason.SIGNATURE_INVALID


@pytest.mark.unit
@pytest.mark.asyncio
async def test_in_memory_verifier_raises_with_configured_reason() -> None:
    verifier = InMemoryTokenVerifier(raise_reason=TokenRejectReason.EXPIRED)

    with pytest.raises(InvalidTokenError) as exc_info:
        await verifier.verify("any-bearer")

    assert exc_info.value.reason == TokenRejectReason.EXPIRED


@pytest.mark.unit
def test_in_memory_verifier_rejects_both_mapping_and_raise_reason() -> None:
    with pytest.raises(ValueError, match="mutually exclusive"):
        InMemoryTokenVerifier(
            mapping={_BEARER: _USER_ID},
            raise_reason=TokenRejectReason.EXPIRED,
        )

# mypy: warn_unused_ignores = False
"""SupabaseJwtVerifier — happy-path integration tests (Slice 3a, AC#9 subset).

Tests use a fixture-controlled RSA keypair so the verifier-under-test can
verify tokens it knows the signing key for, without any Supabase round-trip.
Three scenarios: valid JWT yields UserId; wrong-signature rejects; non-UUID
sub rejects. Slice 3b extends to iss/aud/exp claim validation.
"""

from __future__ import annotations

import time
from typing import TYPE_CHECKING, Any
from uuid import UUID

import jwt
import pytest
from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric import rsa

from altune.adapters.outbound.auth.supabase_jwt_verifier import (  # type: ignore[import-not-found]
    SupabaseJwtVerifier,
)
from altune.application.auth.exceptions import (  # type: ignore[import-not-found]
    InvalidTokenError,
    TokenRejectReason,
)

if TYPE_CHECKING:
    from cryptography.hazmat.primitives.asymmetric.rsa import RSAPrivateKey

_ISS = "https://fixture.supabase.co/auth/v1"
_AUD = "authenticated"
_KID = "test-kid-1"
_USER_UUID_STR = "00000000-0000-0000-0000-000000000042"


def _make_jwks(private_key: RSAPrivateKey, kid: str = _KID) -> dict[str, Any]:
    """Build a JWKS dict containing the public side of `private_key`."""
    jwk = jwt.algorithms.RSAAlgorithm.to_jwk(private_key.public_key(), as_dict=True)
    jwk["kid"] = kid
    jwk["use"] = "sig"
    jwk["alg"] = "RS256"
    return {"keys": [jwk]}


def _sign(
    private_key: RSAPrivateKey,
    *,
    sub: str = _USER_UUID_STR,
    iss: str = _ISS,
    aud: str = _AUD,
    exp_offset: int = 3600,
    kid: str = _KID,
) -> str:
    now = int(time.time())
    payload = {
        "sub": sub,
        "iss": iss,
        "aud": aud,
        "exp": now + exp_offset,
        "iat": now,
    }
    pem = private_key.private_bytes(
        encoding=serialization.Encoding.PEM,
        format=serialization.PrivateFormat.PKCS8,
        encryption_algorithm=serialization.NoEncryption(),
    )
    return jwt.encode(payload, pem, algorithm="RS256", headers={"kid": kid})


@pytest.fixture
def private_key() -> RSAPrivateKey:
    return rsa.generate_private_key(public_exponent=65537, key_size=2048)


@pytest.fixture
def other_private_key() -> RSAPrivateKey:
    return rsa.generate_private_key(public_exponent=65537, key_size=2048)


@pytest.fixture
def verifier(private_key: RSAPrivateKey) -> SupabaseJwtVerifier:
    jwks = _make_jwks(private_key)
    return SupabaseJwtVerifier(
        iss_expected=_ISS,
        aud_expected=_AUD,
        jwks_provider=lambda: jwks,
    )


@pytest.mark.integration
@pytest.mark.asyncio
async def test_verifier_returns_user_id_for_valid_jwt(
    verifier: SupabaseJwtVerifier, private_key: RSAPrivateKey
) -> None:
    token = _sign(private_key)

    result = await verifier.verify(token)

    assert result.value == UUID(_USER_UUID_STR)


@pytest.mark.integration
@pytest.mark.asyncio
async def test_verifier_rejects_signature_mismatch(
    verifier: SupabaseJwtVerifier, other_private_key: RSAPrivateKey
) -> None:
    # Token signed with a key NOT in the verifier's JWKS.
    token = _sign(other_private_key)

    with pytest.raises(InvalidTokenError) as exc_info:
        await verifier.verify(token)

    assert exc_info.value.reason == TokenRejectReason.SIGNATURE_INVALID


@pytest.mark.integration
@pytest.mark.asyncio
async def test_verifier_rejects_non_uuid_sub(
    verifier: SupabaseJwtVerifier, private_key: RSAPrivateKey
) -> None:
    token = _sign(private_key, sub="not-a-uuid")

    with pytest.raises(InvalidTokenError) as exc_info:
        await verifier.verify(token)

    assert exc_info.value.reason == TokenRejectReason.CLAIM_INVALID_SUB

# mypy: warn_unused_ignores = False
"""SupabaseJwtVerifier — claim validation matrix (Slice 3b, AC#9).

Builds on the Slice 3a happy-path fixtures: same RSA-keypair + JWKS pattern,
plus parametrized cases for exp / iss / aud rejection. 30s leeway is asserted
by an explicit "within window" test that should pass even with exp very
slightly in the past.
"""

from __future__ import annotations

import time
from typing import TYPE_CHECKING, Any

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
    payload = {"sub": sub, "iss": iss, "aud": aud, "exp": now + exp_offset, "iat": now}
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
def verifier(private_key: RSAPrivateKey) -> SupabaseJwtVerifier:
    jwks = _make_jwks(private_key)
    return SupabaseJwtVerifier(
        iss_expected=_ISS, aud_expected=_AUD, jwks_provider=lambda: jwks
    )


@pytest.mark.integration
@pytest.mark.asyncio
async def test_verifier_rejects_expired_jwt_beyond_leeway(
    verifier: SupabaseJwtVerifier, private_key: RSAPrivateKey
) -> None:
    # 60s in the past — well beyond the 30s leeway window.
    token = _sign(private_key, exp_offset=-60)

    with pytest.raises(InvalidTokenError) as exc_info:
        await verifier.verify(token)

    assert exc_info.value.reason == TokenRejectReason.EXPIRED


@pytest.mark.integration
@pytest.mark.asyncio
async def test_verifier_accepts_jwt_within_leeway_window(
    verifier: SupabaseJwtVerifier, private_key: RSAPrivateKey
) -> None:
    # 10s in the past — inside the 30s leeway window.
    token = _sign(private_key, exp_offset=-10)

    result = await verifier.verify(token)

    assert str(result.value) == _USER_UUID_STR


@pytest.mark.integration
@pytest.mark.asyncio
async def test_verifier_rejects_wrong_iss(
    verifier: SupabaseJwtVerifier, private_key: RSAPrivateKey
) -> None:
    token = _sign(private_key, iss="https://other.supabase.co/auth/v1")

    with pytest.raises(InvalidTokenError) as exc_info:
        await verifier.verify(token)

    assert exc_info.value.reason == TokenRejectReason.CLAIM_INVALID_ISS


@pytest.mark.integration
@pytest.mark.asyncio
async def test_verifier_rejects_wrong_aud(
    verifier: SupabaseJwtVerifier, private_key: RSAPrivateKey
) -> None:
    token = _sign(private_key, aud="other-audience")

    with pytest.raises(InvalidTokenError) as exc_info:
        await verifier.verify(token)

    assert exc_info.value.reason == TokenRejectReason.CLAIM_INVALID_AUD

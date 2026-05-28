# mypy: ignore_errors = True
"""GET /v1/tracks returns 401 across the auth-rejection matrix (Slice 5, AC#7).

Parameterized over the four AC#7 cases:
- no Authorization header
- malformed Authorization header
- JWT signed by a key the verifier doesn't trust (signature invalid)
- JWT past exp (beyond leeway)

Wrong-iss / wrong-aud are NOT exercised here (AC#9 adapter integration tests
cover those — exercising at this layer would require controlling the live
project's signing key).

The test injects a SupabaseJwtVerifier with a fixture RSA keypair via
`app.state.token_verifier` so we know exactly which keys are trusted. DB is
not seeded — auth fails before any DB access.
"""

from __future__ import annotations

import time
from typing import Any

import jwt
import pytest
from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric import rsa
from fastapi.testclient import TestClient

from altune.adapters.outbound.auth.supabase_jwt_verifier import SupabaseJwtVerifier
from altune.platform.app import create_app
from altune.platform.config import Settings

_ISS = "https://fixture.supabase.co/auth/v1"
_AUD = "authenticated"
_KID = "test-kid-1"
_SUB = "00000000-0000-0000-0000-000000000042"


def _make_jwks(private_key: Any, kid: str = _KID) -> dict[str, Any]:
    jwk = jwt.algorithms.RSAAlgorithm.to_jwk(private_key.public_key(), as_dict=True)
    jwk["kid"] = kid
    jwk["use"] = "sig"
    jwk["alg"] = "RS256"
    return {"keys": [jwk]}


def _sign(private_key: Any, *, sub: str = _SUB, exp_offset: int = 3600, kid: str = _KID) -> str:
    now = int(time.time())
    payload = {"sub": sub, "iss": _ISS, "aud": _AUD, "exp": now + exp_offset, "iat": now}
    pem = private_key.private_bytes(
        encoding=serialization.Encoding.PEM,
        format=serialization.PrivateFormat.PKCS8,
        encryption_algorithm=serialization.NoEncryption(),
    )
    return jwt.encode(payload, pem, algorithm="RS256", headers={"kid": kid})


@pytest.fixture
def trusted_key() -> Any:
    return rsa.generate_private_key(public_exponent=65537, key_size=2048)


@pytest.fixture
def untrusted_key() -> Any:
    return rsa.generate_private_key(public_exponent=65537, key_size=2048)


@pytest.fixture
def app_with_fixture_verifier(trusted_key: Any) -> Any:
    """Construct the app and override app.state.token_verifier with a
    fixture-controlled verifier whose JWKS contains only `trusted_key`."""
    settings = Settings(
        _env_file=None,
        env="test",
        supabase_project_url=_ISS.rsplit("/auth/v1", 1)[0],
        supabase_jwt_aud=_AUD,
        supabase_jwt_jwks_url="https://fixture.supabase.co/auth/v1/keys",
    )
    fastapi_app = create_app(settings=settings)
    # Replace the lifespan-constructed verifier (which fetched empty JWKS due
    # to unresolvable fixture URL) with our fixture-controlled instance.
    jwks = _make_jwks(trusted_key)
    fastapi_app.state.token_verifier = SupabaseJwtVerifier(
        iss_expected=_ISS, aud_expected=_AUD, jwks_provider=lambda: jwks
    )
    return fastapi_app


@pytest.mark.e2e
def test_get_tracks_returns_401_without_authorization_header(
    app_with_fixture_verifier: Any,
) -> None:
    with TestClient(app_with_fixture_verifier) as client:
        response = client.get("/v1/tracks")
    assert response.status_code == 401


@pytest.mark.e2e
def test_get_tracks_returns_401_with_malformed_bearer(app_with_fixture_verifier: Any) -> None:
    with TestClient(app_with_fixture_verifier) as client:
        response = client.get("/v1/tracks", headers={"Authorization": "NotBearer foo"})
    assert response.status_code == 401


@pytest.mark.e2e
def test_get_tracks_returns_401_with_bad_signature_jwt(
    app_with_fixture_verifier: Any, untrusted_key: Any
) -> None:
    token = _sign(untrusted_key)
    with TestClient(app_with_fixture_verifier) as client:
        response = client.get("/v1/tracks", headers={"Authorization": f"Bearer {token}"})
    assert response.status_code == 401


@pytest.mark.e2e
def test_get_tracks_returns_401_with_expired_jwt(
    app_with_fixture_verifier: Any, trusted_key: Any
) -> None:
    # 60s in the past — beyond the 30s leeway.
    token = _sign(trusted_key, exp_offset=-60)
    with TestClient(app_with_fixture_verifier) as client:
        response = client.get("/v1/tracks", headers={"Authorization": f"Bearer {token}"})
    assert response.status_code == 401


# Sanity-check (proving the 401 tests pin auth rejection, not endpoint
# unavailability) is left to Slice 7's per-user-isolation e2e, which seeds
# real DB rows and exercises the full happy path with a valid JWT. Slice 5
# focuses strictly on the rejection matrix.

# mypy: ignore_errors = True
"""GET /health stays unauthenticated after Slice 4's auth swap (Slice 6, AC#11).

Regression guard. The dependency-body swap in platform/auth.py is exactly
the kind of change that could accidentally tighten the /health endpoint;
this test pins the contract.
"""

from __future__ import annotations

import pytest
from fastapi.testclient import TestClient

from altune.platform.app import create_app
from altune.platform.config import Settings


@pytest.mark.e2e
def test_health_returns_200_without_authorization_header_after_auth_swap() -> None:
    settings = Settings(
        _env_file=None,
        env="test",
        supabase_project_url="https://fixture.supabase.co",
        supabase_jwt_jwks_url="https://fixture.supabase.co/auth/v1/keys",
    )
    app = create_app(settings=settings)
    with TestClient(app) as client:
        response = client.get("/health")
    assert response.status_code == 200
    body = response.json()
    assert body["status"] == "ok"

"""Smoke test: the FastAPI app can be constructed and the /health route responds.

This is the only test on day 1. Real tests land as features arrive.
"""

from __future__ import annotations

import pytest
from fastapi.testclient import TestClient

from altune.platform.app import create_app


@pytest.mark.unit
def test_app_starts() -> None:
    app = create_app()
    assert app.title == "Altune API"


@pytest.mark.unit
def test_health_endpoint_returns_ok() -> None:
    app = create_app()
    client = TestClient(app)
    response = client.get("/health")
    assert response.status_code == 200
    body = response.json()
    assert body["status"] == "ok"
    assert "version" in body

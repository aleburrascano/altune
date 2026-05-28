# mypy: warn_unused_ignores = False, disable_error_code = "no-any-return,untyped-decorator"
"""DeezerSearchAdapter — slice 17 respx-mocked integration tests."""

from __future__ import annotations

import json
from pathlib import Path

import httpx
import pytest
import respx

from altune.adapters.outbound.discovery.deezer.adapter import DeezerSearchAdapter
from altune.domain.discovery.provider import ProviderName
from altune.domain.discovery.provider_status import ProviderStatus
from altune.domain.discovery.result_kind import ResultKind

_FIXTURE = (
    Path(__file__).resolve().parents[4]
    / "fixtures"
    / "discovery"
    / "deezer"
    / "track_search.json"
)


@pytest.fixture
def deezer_payload() -> dict:  # type: ignore[type-arg]
    return json.loads(_FIXTURE.read_text(encoding="utf-8"))


@pytest.mark.integration
@pytest.mark.asyncio
@respx.mock
async def test_deezer_adapter_translates_track_search_response(
    deezer_payload: dict,  # type: ignore[type-arg]
) -> None:
    respx.get("https://api.deezer.com/search/track").mock(
        return_value=httpx.Response(200, json=deezer_payload)
    )
    async with httpx.AsyncClient() as client:
        adapter = DeezerSearchAdapter(client=client)
        resp = await adapter.search(
            "the beatles let it be", frozenset({ResultKind.TRACK}), limit=5
        )
    assert resp.provider_name == "deezer"
    assert resp.status is ProviderStatus.OK
    assert len(resp.results) == 5
    first = resp.results[0]
    assert first.kind is ResultKind.TRACK
    assert first.title == "Let It Be (Remastered 2009)"
    assert first.subtitle == "The Beatles"
    assert first.sources[0].provider is ProviderName.DEEZER
    assert first.extras["isrc"] == "GBAYE0601713"
    assert first.extras["duration_seconds"] == 243
    assert first.extras["preview_url"] is None


@pytest.mark.integration
@pytest.mark.asyncio
@respx.mock
async def test_deezer_adapter_drops_malformed_track() -> None:
    bad_payload = {
        "data": [
            {"id": 1, "title": None, "artist": {"name": "X"}, "link": "https://x/1"},  # missing title
            {
                "id": 2,
                "title": "Good Track",
                "artist": {"name": "Artist"},
                "link": "https://x/2",
            },
        ],
        "total": 2,
    }
    respx.get("https://api.deezer.com/search/track").mock(
        return_value=httpx.Response(200, json=bad_payload)
    )
    async with httpx.AsyncClient() as client:
        adapter = DeezerSearchAdapter(client=client)
        resp = await adapter.search("q", frozenset({ResultKind.TRACK}), limit=5)
    assert len(resp.results) == 1
    assert resp.results[0].title == "Good Track"


@pytest.mark.integration
@pytest.mark.asyncio
@respx.mock
async def test_deezer_adapter_maps_429_to_rate_limited() -> None:
    respx.get("https://api.deezer.com/search/track").mock(
        return_value=httpx.Response(429, text="Too Many Requests")
    )
    async with httpx.AsyncClient() as client:
        adapter = DeezerSearchAdapter(client=client)
        resp = await adapter.search("q", frozenset({ResultKind.TRACK}), limit=5)
    assert resp.status is ProviderStatus.RATE_LIMITED


@pytest.mark.integration
@pytest.mark.asyncio
@respx.mock
async def test_deezer_adapter_maps_5xx_to_error() -> None:
    respx.get("https://api.deezer.com/search/track").mock(
        return_value=httpx.Response(503, text="Service Unavailable")
    )
    async with httpx.AsyncClient() as client:
        adapter = DeezerSearchAdapter(client=client)
        resp = await adapter.search("q", frozenset({ResultKind.TRACK}), limit=5)
    assert resp.status is ProviderStatus.ERROR


@pytest.mark.integration
@pytest.mark.asyncio
async def test_deezer_adapter_returns_empty_for_non_track_kinds() -> None:
    async with httpx.AsyncClient() as client:
        adapter = DeezerSearchAdapter(client=client)
        resp = await adapter.search(
            "q", frozenset({ResultKind.ARTIST, ResultKind.ALBUM}), limit=5
        )
    assert resp.status is ProviderStatus.OK
    assert resp.results == ()

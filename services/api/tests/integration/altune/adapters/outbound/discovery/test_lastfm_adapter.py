# mypy: warn_unused_ignores = False, disable_error_code = "no-any-return,untyped-decorator"
"""LastFmSearchAdapter — slice 20 respx-mocked integration tests."""

from __future__ import annotations

import json
from pathlib import Path

import httpx
import pytest
import respx

from altune.adapters.outbound.discovery.lastfm.adapter import LastFmSearchAdapter
from altune.domain.discovery.provider import ProviderName
from altune.domain.discovery.provider_status import ProviderStatus
from altune.domain.discovery.result_kind import ResultKind

_FIXTURE = (
    Path(__file__).resolve().parents[4] / "fixtures" / "discovery" / "lastfm" / "track_search.json"
)

_API_KEY = "fixture-api-key"


@pytest.fixture
def lastfm_payload() -> dict:  # type: ignore[type-arg]
    return json.loads(_FIXTURE.read_text(encoding="utf-8"))


@pytest.mark.integration
@pytest.mark.asyncio
@respx.mock
async def test_lastfm_adapter_translates_track_search(
    lastfm_payload: dict,  # type: ignore[type-arg]
) -> None:
    route = respx.get("https://ws.audioscrobbler.com/2.0/").mock(
        return_value=httpx.Response(200, json=lastfm_payload)
    )
    async with httpx.AsyncClient() as client:
        adapter = LastFmSearchAdapter(client=client, api_key=_API_KEY)
        resp = await adapter.search("let it be", frozenset({ResultKind.TRACK}), limit=5)
    assert route.called
    request_url = str(route.calls.last.request.url)
    assert "api_key=fixture-api-key" in request_url
    assert "method=track.search" in request_url
    assert resp.provider_name == "lastfm"
    assert resp.status is ProviderStatus.OK
    assert len(resp.results) == 5
    first = resp.results[0]
    assert first.kind is ResultKind.TRACK
    assert first.title == "Let It Be"
    assert first.subtitle == "The Beatles"
    assert first.sources[0].provider is ProviderName.LASTFM
    assert first.image_url is not None
    assert "300x300" in first.image_url
    assert first.extras["listeners"] == 1161107
    assert first.extras["mbid"] == "911fb8d2-f7b4-4cb9-8a8e-656881773917"
    assert first.extras["preview_url"] is None


@pytest.mark.integration
@pytest.mark.asyncio
@respx.mock
async def test_lastfm_adapter_drops_malformed_track() -> None:
    bad_payload = {
        "results": {
            "trackmatches": {
                "track": [
                    {"name": None, "artist": "X", "url": "https://x"},
                    {
                        "name": "Good Track",
                        "artist": "Good Artist",
                        "url": "https://x/good",
                    },
                ]
            }
        }
    }
    respx.get("https://ws.audioscrobbler.com/2.0/").mock(
        return_value=httpx.Response(200, json=bad_payload)
    )
    async with httpx.AsyncClient() as client:
        adapter = LastFmSearchAdapter(client=client, api_key=_API_KEY)
        resp = await adapter.search("q", frozenset({ResultKind.TRACK}), limit=5)
    assert len(resp.results) == 1
    assert resp.results[0].title == "Good Track"


@pytest.mark.integration
@pytest.mark.asyncio
@respx.mock
async def test_lastfm_adapter_handles_single_track_as_dict() -> None:
    # When there's exactly one result, last.fm sometimes returns a bare
    # dict (not a list of one). Adapter must tolerate.
    one_track_payload = {
        "results": {
            "trackmatches": {
                "track": {
                    "name": "Solo",
                    "artist": "Solo Artist",
                    "url": "https://x/solo",
                }
            }
        }
    }
    respx.get("https://ws.audioscrobbler.com/2.0/").mock(
        return_value=httpx.Response(200, json=one_track_payload)
    )
    async with httpx.AsyncClient() as client:
        adapter = LastFmSearchAdapter(client=client, api_key=_API_KEY)
        resp = await adapter.search("q", frozenset({ResultKind.TRACK}), limit=5)
    assert len(resp.results) == 1
    assert resp.results[0].title == "Solo"


@pytest.mark.integration
@pytest.mark.asyncio
@respx.mock
async def test_lastfm_adapter_maps_429_to_rate_limited() -> None:
    respx.get("https://ws.audioscrobbler.com/2.0/").mock(
        return_value=httpx.Response(429, text="Too Many Requests")
    )
    async with httpx.AsyncClient() as client:
        adapter = LastFmSearchAdapter(client=client, api_key=_API_KEY)
        resp = await adapter.search("q", frozenset({ResultKind.TRACK}), limit=5)
    assert resp.status is ProviderStatus.RATE_LIMITED


@pytest.mark.integration
@pytest.mark.asyncio
@respx.mock
async def test_lastfm_adapter_maps_500_to_error() -> None:
    respx.get("https://ws.audioscrobbler.com/2.0/").mock(
        return_value=httpx.Response(500, text="Internal Server Error")
    )
    async with httpx.AsyncClient() as client:
        adapter = LastFmSearchAdapter(client=client, api_key=_API_KEY)
        resp = await adapter.search("q", frozenset({ResultKind.TRACK}), limit=5)
    assert resp.status is ProviderStatus.ERROR


@pytest.mark.integration
@pytest.mark.asyncio
async def test_lastfm_adapter_returns_empty_for_non_track_kinds() -> None:
    async with httpx.AsyncClient() as client:
        adapter = LastFmSearchAdapter(client=client, api_key=_API_KEY)
        resp = await adapter.search("q", frozenset({ResultKind.ARTIST}), limit=5)
    assert resp.status is ProviderStatus.OK
    assert resp.results == ()

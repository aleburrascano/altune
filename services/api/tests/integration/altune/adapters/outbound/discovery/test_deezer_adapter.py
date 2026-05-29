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
    # discover-music-v2: preview + popularity are now populated.
    assert isinstance(first.extras["preview_url"], str)
    assert isinstance(first.extras["popularity"], float)


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
@respx.mock
async def test_deezer_adapter_translates_albums_and_artists() -> None:
    # discover-music-v2: album + artist search alongside tracks.
    respx.get("https://api.deezer.com/search/album").mock(
        return_value=httpx.Response(
            200,
            json={
                "data": [
                    {
                        "id": 1261474,
                        "title": "REST IN BASS",
                        "artist": {"name": "Che"},
                        "link": "https://www.deezer.com/album/1261474",
                        "cover_xl": "https://x/cover.jpg",
                        "nb_tracks": 18,
                    }
                ]
            },
        )
    )
    respx.get("https://api.deezer.com/search/artist").mock(
        return_value=httpx.Response(
            200,
            json={
                "data": [
                    {
                        "id": 99,
                        "name": "Che",
                        "link": "https://www.deezer.com/artist/99",
                        "picture_xl": "https://x/pic.jpg",
                        "nb_fan": 250000,
                    }
                ]
            },
        )
    )
    async with httpx.AsyncClient() as client:
        adapter = DeezerSearchAdapter(client=client)
        resp = await adapter.search(
            "che rest in bass", frozenset({ResultKind.ALBUM, ResultKind.ARTIST}), limit=5
        )
    assert resp.status is ProviderStatus.OK
    kinds = {r.kind for r in resp.results}
    assert kinds == {ResultKind.ALBUM, ResultKind.ARTIST}
    album = next(r for r in resp.results if r.kind is ResultKind.ALBUM)
    assert album.title == "REST IN BASS"
    assert album.subtitle == "Che"
    assert album.extras["track_count"] == 18
    artist = next(r for r in resp.results if r.kind is ResultKind.ARTIST)
    assert artist.title == "Che"
    assert artist.subtitle is None
    assert isinstance(artist.extras["popularity"], float)

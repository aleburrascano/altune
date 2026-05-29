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
    # discover-music-v2: listeners log-normalized into popularity.
    assert isinstance(first.extras["popularity"], float)


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
@respx.mock
async def test_lastfm_adapter_translates_albums_but_never_artists() -> None:
    # discover-music-v2: Last.fm serves albums (+ tracks) but NOT artists — its
    # artist.search DB is crowd-scrobbled junk. Even when ARTIST is requested,
    # the adapter must not call artist.search nor return artist results.
    album_payload = {
        "results": {
            "albummatches": {
                "album": [
                    {
                        "name": "REST IN BASS",
                        "artist": "Che",
                        "url": "https://www.last.fm/music/Che/REST+IN+BASS",
                        "mbid": "album-mbid-1",
                        "image": [
                            {"#text": "https://x/album-small.png", "size": "small"},
                            {"#text": "https://x/album-xl.png", "size": "extralarge"},
                        ],
                    }
                ]
            }
        }
    }
    called_methods: list[str] = []

    def _by_method(request: httpx.Request) -> httpx.Response:
        method = request.url.params.get("method") or ""
        called_methods.append(method)
        if method == "album.search":
            return httpx.Response(200, json=album_payload)
        return httpx.Response(200, json={"results": {}})

    respx.get("https://ws.audioscrobbler.com/2.0/").mock(side_effect=_by_method)
    async with httpx.AsyncClient() as client:
        adapter = LastFmSearchAdapter(client=client, api_key=_API_KEY)
        resp = await adapter.search(
            "che rest in bass", frozenset({ResultKind.ALBUM, ResultKind.ARTIST}), limit=5
        )
    assert resp.status is ProviderStatus.OK
    assert "artist.search" not in called_methods  # never queried
    assert {r.kind for r in resp.results} == {ResultKind.ALBUM}

    album = resp.results[0]
    assert album.title == "REST IN BASS"
    assert album.subtitle == "Che"
    assert album.image_url == "https://x/album-xl.png"
    assert album.sources[0].provider is ProviderName.LASTFM
    assert album.sources[0].external_id == "album-mbid-1"


@pytest.mark.integration
@pytest.mark.asyncio
@respx.mock
async def test_lastfm_resolve_popularity_from_getinfo() -> None:
    # getInfo play counts are the uniform popularity signal (keyed by artist+title).
    respx.get("https://ws.audioscrobbler.com/2.0/").mock(
        return_value=httpx.Response(
            200, json={"track": {"name": "Creep", "playcount": "61244353", "listeners": "4114888"}}
        )
    )
    async with httpx.AsyncClient() as client:
        adapter = LastFmSearchAdapter(client=client, api_key=_API_KEY)
        pop = await adapter.resolve_popularity(ResultKind.TRACK, "Creep", "Radiohead")
    assert pop is not None
    assert 0.0 < pop <= 1.0  # log-normalized playcount


@pytest.mark.integration
@pytest.mark.asyncio
@respx.mock
async def test_lastfm_resolve_popularity_none_on_error_body() -> None:
    respx.get("https://ws.audioscrobbler.com/2.0/").mock(
        return_value=httpx.Response(200, json={"error": 6, "message": "Track not found"})
    )
    async with httpx.AsyncClient() as client:
        adapter = LastFmSearchAdapter(client=client, api_key=_API_KEY)
        pop = await adapter.resolve_popularity(ResultKind.TRACK, "Nope", "Nobody")
    assert pop is None

# mypy: warn_unused_ignores = False, disable_error_code = "no-any-return,untyped-decorator"
"""MusicBrainzSearchAdapter — slice 19 respx-mocked integration tests."""

from __future__ import annotations

import json
from pathlib import Path

import httpx
import pytest
import respx

from altune.adapters.outbound.discovery.musicbrainz.adapter import MusicBrainzSearchAdapter
from altune.domain.discovery.provider import ProviderName
from altune.domain.discovery.provider_status import ProviderStatus
from altune.domain.discovery.result_kind import ResultKind

_FIXTURE = (
    Path(__file__).resolve().parents[4]
    / "fixtures"
    / "discovery"
    / "musicbrainz"
    / "recording_search.json"
)

_UA = "altune-test/0.1 ( mailto:dev@altune.test )"


@pytest.fixture
def mb_payload() -> dict:  # type: ignore[type-arg]
    return json.loads(_FIXTURE.read_text(encoding="utf-8"))


@pytest.mark.integration
@pytest.mark.asyncio
@respx.mock
async def test_musicbrainz_adapter_translates_recording_search(
    mb_payload: dict,  # type: ignore[type-arg]
) -> None:
    route = respx.get("https://musicbrainz.org/ws/2/recording").mock(
        return_value=httpx.Response(200, json=mb_payload)
    )
    async with httpx.AsyncClient(headers={"User-Agent": _UA}) as client:
        adapter = MusicBrainzSearchAdapter(client=client)
        resp = await adapter.search("the beatles let it be", frozenset({ResultKind.TRACK}), limit=5)
    assert route.called
    request_ua = route.calls.last.request.headers.get("user-agent")
    assert request_ua == _UA
    assert resp.provider_name == "musicbrainz"
    assert resp.status is ProviderStatus.OK
    assert len(resp.results) == 5
    first = resp.results[0]
    assert first.kind is ResultKind.TRACK
    assert first.title == "Let It Be (Let It Be rehearsals)"
    assert first.subtitle == "The Beatles"
    assert first.sources[0].provider is ProviderName.MUSICBRAINZ
    assert first.sources[0].external_id == "b925d2a3-3bd3-4dee-b342-8b9d27ecaebc"
    assert first.extras["mbid"] == "b925d2a3-3bd3-4dee-b342-8b9d27ecaebc"
    assert first.extras["duration_seconds"] == 170
    assert first.extras["isrc"] is None
    assert first.extras["preview_url"] is None


@pytest.mark.integration
@pytest.mark.asyncio
@respx.mock
async def test_musicbrainz_adapter_drops_malformed_recording() -> None:
    bad_payload = {
        "recordings": [
            {"id": "111", "title": None, "artist-credit": [{"name": "X"}]},
            {
                "id": "222",
                "title": "Good Title",
                "artist-credit": [{"name": "Good Artist"}],
                "length": 200000,
            },
        ],
    }
    respx.get("https://musicbrainz.org/ws/2/recording").mock(
        return_value=httpx.Response(200, json=bad_payload)
    )
    async with httpx.AsyncClient() as client:
        adapter = MusicBrainzSearchAdapter(client=client)
        resp = await adapter.search("q", frozenset({ResultKind.TRACK}), limit=5)
    assert len(resp.results) == 1
    assert resp.results[0].title == "Good Title"


@pytest.mark.integration
@pytest.mark.asyncio
@respx.mock
async def test_musicbrainz_adapter_maps_503_to_rate_limited() -> None:
    respx.get("https://musicbrainz.org/ws/2/recording").mock(
        return_value=httpx.Response(503, text="Service Unavailable")
    )
    async with httpx.AsyncClient() as client:
        adapter = MusicBrainzSearchAdapter(client=client)
        resp = await adapter.search("q", frozenset({ResultKind.TRACK}), limit=5)
    assert resp.status is ProviderStatus.RATE_LIMITED


@pytest.mark.integration
@pytest.mark.asyncio
@respx.mock
async def test_musicbrainz_adapter_maps_500_to_error() -> None:
    respx.get("https://musicbrainz.org/ws/2/recording").mock(
        return_value=httpx.Response(500, text="Internal Server Error")
    )
    async with httpx.AsyncClient() as client:
        adapter = MusicBrainzSearchAdapter(client=client)
        resp = await adapter.search("q", frozenset({ResultKind.TRACK}), limit=5)
    assert resp.status is ProviderStatus.ERROR


@pytest.mark.integration
@pytest.mark.asyncio
@respx.mock
async def test_musicbrainz_adapter_translates_release_group_search() -> None:
    payload = {
        "release-groups": [
            {
                "id": "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
                "title": "Let It Be",
                "artist-credit": [{"name": "The Beatles"}],
                "first-release-date": "1970-05-08",
            },
            {
                "id": "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb",
                "title": "Undated Album",
                "artist-credit": [{"name": "Someone"}],
            },
        ]
    }
    respx.get("https://musicbrainz.org/ws/2/release-group").mock(
        return_value=httpx.Response(200, json=payload)
    )
    async with httpx.AsyncClient() as client:
        adapter = MusicBrainzSearchAdapter(client=client)
        resp = await adapter.search("let it be", frozenset({ResultKind.ALBUM}), limit=5)
    assert resp.status is ProviderStatus.OK
    assert len(resp.results) == 2
    first = resp.results[0]
    assert first.kind is ResultKind.ALBUM
    assert first.title == "Let It Be"
    assert first.subtitle == "The Beatles"
    # discover-music-v3: album art served by Cover Art Archive via the MBID.
    assert first.image_url == (
        "https://coverartarchive.org/release-group/aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa/front-500"
    )
    assert first.extras["mbid"] == "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
    assert first.extras["year"] == "1970"
    assert first.extras["isrc"] is None
    assert first.extras["preview_url"] is None
    assert first.sources[0].provider is ProviderName.MUSICBRAINZ
    assert first.sources[0].external_id == "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
    assert first.sources[0].url == (
        "https://musicbrainz.org/release-group/aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
    )
    assert "popularity" not in first.extras
    # Missing first-release-date → year None.
    assert resp.results[1].extras["year"] is None


@pytest.mark.integration
@pytest.mark.asyncio
@respx.mock
async def test_musicbrainz_adapter_translates_artist_search() -> None:
    payload = {
        "artists": [
            {"id": "cccccccc-cccc-cccc-cccc-cccccccccccc", "name": "The Beatles"},
        ]
    }
    respx.get("https://musicbrainz.org/ws/2/artist").mock(
        return_value=httpx.Response(200, json=payload)
    )
    async with httpx.AsyncClient() as client:
        adapter = MusicBrainzSearchAdapter(client=client)
        resp = await adapter.search("the beatles", frozenset({ResultKind.ARTIST}), limit=5)
    assert resp.status is ProviderStatus.OK
    assert len(resp.results) == 1
    first = resp.results[0]
    assert first.kind is ResultKind.ARTIST
    assert first.title == "The Beatles"
    assert first.subtitle is None
    assert first.image_url is None
    assert first.extras["isrc"] is None
    assert first.extras["preview_url"] is None
    assert "popularity" not in first.extras
    assert first.sources[0].provider is ProviderName.MUSICBRAINZ
    assert first.sources[0].external_id == "cccccccc-cccc-cccc-cccc-cccccccccccc"
    assert first.sources[0].url == (
        "https://musicbrainz.org/artist/cccccccc-cccc-cccc-cccc-cccccccccccc"
    )


@pytest.mark.integration
@pytest.mark.asyncio
async def test_musicbrainz_adapter_returns_empty_for_no_kinds() -> None:
    async with httpx.AsyncClient() as client:
        adapter = MusicBrainzSearchAdapter(client=client)
        resp = await adapter.search("q", frozenset(), limit=5)
    assert resp.status is ProviderStatus.OK
    assert resp.results == ()

# mypy: warn_unused_ignores = False, disable_error_code = "no-any-return,untyped-decorator"
"""iTunesSearchAdapter — respx-mocked integration tests.

iTunes Search API: free, no auth. https://itunes.apple.com/search. No ISRC
in the response, but strong relevance + artwork + preview URLs.
"""

from __future__ import annotations

import json
from pathlib import Path

import httpx
import pytest
import respx

from altune.adapters.outbound.discovery.itunes.adapter import ITunesSearchAdapter
from altune.domain.discovery.provider import ProviderName
from altune.domain.discovery.provider_status import ProviderStatus
from altune.domain.discovery.result_kind import ResultKind

_FIXTURE = (
    Path(__file__).resolve().parents[4] / "fixtures" / "discovery" / "itunes" / "track_search.json"
)


@pytest.fixture
def itunes_payload() -> dict:  # type: ignore[type-arg]
    return json.loads(_FIXTURE.read_text(encoding="utf-8"))


@pytest.mark.integration
@pytest.mark.asyncio
@respx.mock
async def test_itunes_adapter_translates_track_search_response(
    itunes_payload: dict,  # type: ignore[type-arg]
) -> None:
    respx.get("https://itunes.apple.com/search").mock(
        return_value=httpx.Response(200, json=itunes_payload)
    )
    async with httpx.AsyncClient() as client:
        adapter = ITunesSearchAdapter(client=client)
        resp = await adapter.search("africa toto", frozenset({ResultKind.TRACK}), limit=5)
    assert resp.provider_name == "itunes"
    assert resp.status is ProviderStatus.OK
    assert len(resp.results) == 3
    first = resp.results[0]
    assert first.kind is ResultKind.TRACK
    assert first.title == "Africa"
    assert first.subtitle == "TOTO"
    assert first.sources[0].provider is ProviderName.ITUNES
    assert first.sources[0].external_id == "401185800"
    # iTunes carries no ISRC.
    assert first.extras["isrc"] is None
    # preview_url is populated from iTunes (fills the reserved field).
    assert first.extras["preview_url"] == "https://audio-ssl.itunes.apple.com/preview/africa.m4a"
    assert first.extras["duration_seconds"] == 295
    assert first.extras["album"] == "Toto IV"


@pytest.mark.integration
@pytest.mark.asyncio
@respx.mock
async def test_itunes_adapter_upscales_artwork(
    itunes_payload: dict,  # type: ignore[type-arg]
) -> None:
    respx.get("https://itunes.apple.com/search").mock(
        return_value=httpx.Response(200, json=itunes_payload)
    )
    async with httpx.AsyncClient() as client:
        adapter = ITunesSearchAdapter(client=client)
        resp = await adapter.search("africa", frozenset({ResultKind.TRACK}), limit=5)
    # 100x100bb upscaled to 600x600bb.
    assert resp.results[0].image_url is not None
    assert "600x600bb" in resp.results[0].image_url
    assert "100x100" not in resp.results[0].image_url


@pytest.mark.integration
@pytest.mark.asyncio
@respx.mock
async def test_itunes_adapter_drops_malformed_track() -> None:
    bad_payload = {
        "resultCount": 2,
        "results": [
            {"trackId": 1, "trackName": None, "artistName": "X", "trackViewUrl": "https://x/1"},
            {
                "trackId": 2,
                "trackName": "Good Track",
                "artistName": "Artist",
                "trackViewUrl": "https://music.apple.com/song/2",
                "artworkUrl100": "https://x/100x100bb.jpg",
            },
        ],
    }
    respx.get("https://itunes.apple.com/search").mock(
        return_value=httpx.Response(200, json=bad_payload)
    )
    async with httpx.AsyncClient() as client:
        adapter = ITunesSearchAdapter(client=client)
        resp = await adapter.search("q", frozenset({ResultKind.TRACK}), limit=5)
    assert len(resp.results) == 1
    assert resp.results[0].title == "Good Track"


@pytest.mark.integration
@pytest.mark.asyncio
@respx.mock
async def test_itunes_adapter_maps_403_to_error() -> None:
    respx.get("https://itunes.apple.com/search").mock(
        return_value=httpx.Response(403, text="Forbidden")
    )
    async with httpx.AsyncClient() as client:
        adapter = ITunesSearchAdapter(client=client)
        resp = await adapter.search("q", frozenset({ResultKind.TRACK}), limit=5)
    assert resp.status is ProviderStatus.ERROR


@pytest.mark.integration
@pytest.mark.asyncio
@respx.mock
async def test_itunes_adapter_translates_album_and_artist_search() -> None:
    album_payload = {
        "resultCount": 1,
        "results": [
            {
                "wrapperType": "collection",
                "collectionType": "Album",
                "collectionId": 401185795,
                "artistName": "TOTO",
                "collectionName": "Toto IV",
                "collectionViewUrl": "https://music.apple.com/us/album/toto-iv/401185795",
                "artworkUrl100": "https://is1-ssl.mzstatic.com/image/thumb/abc/100x100bb.jpg",
                "trackCount": 10,
            }
        ],
    }
    artist_payload = {
        "resultCount": 1,
        "results": [
            {
                "wrapperType": "artist",
                "artistType": "Artist",
                "artistId": 137250,
                "artistName": "TOTO",
                "artistLinkUrl": "https://music.apple.com/us/artist/toto/137250",
            }
        ],
    }
    # respx matches on URL + params; serve distinct payloads per entity.
    respx.get("https://itunes.apple.com/search", params__contains={"entity": "album"}).mock(
        return_value=httpx.Response(200, json=album_payload)
    )
    respx.get("https://itunes.apple.com/search", params__contains={"entity": "musicArtist"}).mock(
        return_value=httpx.Response(200, json=artist_payload)
    )
    async with httpx.AsyncClient() as client:
        adapter = ITunesSearchAdapter(client=client)
        resp = await adapter.search(
            "toto", frozenset({ResultKind.ALBUM, ResultKind.ARTIST}), limit=5
        )

    assert resp.status is ProviderStatus.OK
    by_kind = {r.kind: r for r in resp.results}

    album = by_kind[ResultKind.ALBUM]
    assert album.title == "Toto IV"
    assert album.subtitle == "TOTO"
    assert album.extras["track_count"] == 10
    assert album.extras["isrc"] is None
    assert album.extras["preview_url"] is None
    assert album.image_url is not None
    assert "600x600bb" in album.image_url
    assert album.sources[0].external_id == "401185795"
    # iTunes has no popularity field.
    assert "popularity" not in album.extras

    artist = by_kind[ResultKind.ARTIST]
    assert artist.title == "TOTO"
    assert artist.subtitle is None
    assert artist.image_url is None
    assert artist.sources[0].external_id == "137250"
    assert "popularity" not in artist.extras


@pytest.mark.integration
@pytest.mark.asyncio
@respx.mock
async def test_itunes_adapter_constructs_artist_url_when_link_missing() -> None:
    artist_payload = {
        "resultCount": 1,
        "results": [
            {"wrapperType": "artist", "artistId": 137250, "artistName": "TOTO"},
        ],
    }
    respx.get("https://itunes.apple.com/search").mock(
        return_value=httpx.Response(200, json=artist_payload)
    )
    async with httpx.AsyncClient() as client:
        adapter = ITunesSearchAdapter(client=client)
        resp = await adapter.search("toto", frozenset({ResultKind.ARTIST}), limit=5)
    assert resp.results[0].sources[0].url == "https://music.apple.com/artist/137250"

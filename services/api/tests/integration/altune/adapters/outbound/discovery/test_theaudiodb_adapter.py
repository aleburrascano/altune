# mypy: warn_unused_ignores = False, disable_error_code = "no-any-return,untyped-decorator"
"""TheAudioDBSearchAdapter — respx-mocked integration tests.

TheAudioDB: free key "123", 30 req/min. Free-text search supports ARTIST
only (search.php?s=). Also implements ArtworkResolver.
"""

from __future__ import annotations

import httpx
import pytest
import respx

from altune.adapters.outbound.discovery.theaudiodb.adapter import TheAudioDBSearchAdapter
from altune.domain.discovery.provider import ProviderName
from altune.domain.discovery.provider_status import ProviderStatus
from altune.domain.discovery.result_kind import ResultKind

_SEARCH_URL = "https://www.theaudiodb.com/api/v1/json/123/search.php"
_ALBUM_URL = "https://www.theaudiodb.com/api/v1/json/123/searchalbum.php"
_TRACK_URL = "https://www.theaudiodb.com/api/v1/json/123/searchtrack.php"


@pytest.mark.integration
@pytest.mark.asyncio
@respx.mock
async def test_theaudiodb_adapter_translates_artist_search() -> None:
    payload = {
        "artists": [
            {
                "idArtist": "111239",
                "strArtist": "Coldplay",
                "strArtistThumb": "https://www.theaudiodb.com/images/media/artist/thumb/cp.jpg",
                "strArtistWideThumb": "https://www.theaudiodb.com/images/media/artist/wide/cp.jpg",
            }
        ]
    }
    respx.get(_SEARCH_URL).mock(return_value=httpx.Response(200, json=payload))
    async with httpx.AsyncClient() as client:
        adapter = TheAudioDBSearchAdapter(client=client)
        resp = await adapter.search("coldplay", frozenset({ResultKind.ARTIST}), limit=5)
    assert resp.provider_name == "theaudiodb"
    assert resp.status is ProviderStatus.OK
    assert len(resp.results) == 1
    first = resp.results[0]
    assert first.kind is ResultKind.ARTIST
    assert first.title == "Coldplay"
    assert first.subtitle is None
    assert first.image_url == "https://www.theaudiodb.com/images/media/artist/thumb/cp.jpg"
    assert first.sources[0].provider is ProviderName.THEAUDIODB
    assert first.sources[0].external_id == "111239"
    assert "popularity" not in first.extras


@pytest.mark.integration
@pytest.mark.asyncio
@respx.mock
async def test_theaudiodb_adapter_falls_back_to_wide_thumb() -> None:
    payload = {
        "artists": [
            {
                "idArtist": "1",
                "strArtist": "Artist",
                "strArtistThumb": None,
                "strArtistWideThumb": "https://x/wide.jpg",
            }
        ]
    }
    respx.get(_SEARCH_URL).mock(return_value=httpx.Response(200, json=payload))
    async with httpx.AsyncClient() as client:
        adapter = TheAudioDBSearchAdapter(client=client)
        resp = await adapter.search("artist", frozenset({ResultKind.ARTIST}), limit=5)
    assert resp.results[0].image_url == "https://x/wide.jpg"


@pytest.mark.integration
@pytest.mark.asyncio
@respx.mock
async def test_theaudiodb_adapter_returns_empty_for_track_only_kinds() -> None:
    route = respx.get(_SEARCH_URL).mock(return_value=httpx.Response(200, json={"artists": []}))
    async with httpx.AsyncClient() as client:
        adapter = TheAudioDBSearchAdapter(client=client)
        resp = await adapter.search("q", frozenset({ResultKind.TRACK}), limit=5)
    assert resp.status is ProviderStatus.OK
    assert resp.results == ()
    # No HTTP call is made for unsupported-only kinds.
    assert not route.called


@pytest.mark.integration
@pytest.mark.asyncio
@respx.mock
async def test_theaudiodb_adapter_handles_null_artists() -> None:
    respx.get(_SEARCH_URL).mock(return_value=httpx.Response(200, json={"artists": None}))
    async with httpx.AsyncClient() as client:
        adapter = TheAudioDBSearchAdapter(client=client)
        resp = await adapter.search("nomatch", frozenset({ResultKind.ARTIST}), limit=5)
    assert resp.status is ProviderStatus.OK
    assert resp.results == ()


@pytest.mark.integration
@pytest.mark.asyncio
@respx.mock
async def test_theaudiodb_adapter_drops_malformed_artist() -> None:
    payload = {
        "artists": [
            {"idArtist": "1", "strArtist": None, "strArtistThumb": "https://x/1.jpg"},
            {"idArtist": "2", "strArtist": "Good", "strArtistThumb": "https://x/2.jpg"},
        ]
    }
    respx.get(_SEARCH_URL).mock(return_value=httpx.Response(200, json=payload))
    async with httpx.AsyncClient() as client:
        adapter = TheAudioDBSearchAdapter(client=client)
        resp = await adapter.search("q", frozenset({ResultKind.ARTIST}), limit=5)
    assert len(resp.results) == 1
    assert resp.results[0].title == "Good"


@pytest.mark.integration
@pytest.mark.asyncio
@respx.mock
async def test_theaudiodb_adapter_maps_429_to_rate_limited() -> None:
    respx.get(_SEARCH_URL).mock(return_value=httpx.Response(429, text="Too Many Requests"))
    async with httpx.AsyncClient() as client:
        adapter = TheAudioDBSearchAdapter(client=client)
        resp = await adapter.search("q", frozenset({ResultKind.ARTIST}), limit=5)
    assert resp.status is ProviderStatus.RATE_LIMITED


@pytest.mark.integration
@pytest.mark.asyncio
@respx.mock
async def test_theaudiodb_resolve_artwork_artist_returns_thumb() -> None:
    payload = {"artists": [{"idArtist": "1", "strArtist": "X", "strArtistThumb": "https://x/a.jpg"}]}
    respx.get(_SEARCH_URL).mock(return_value=httpx.Response(200, json=payload))
    async with httpx.AsyncClient() as client:
        adapter = TheAudioDBSearchAdapter(client=client)
        art = await adapter.resolve_artwork(ResultKind.ARTIST, "X", None)
    assert art == "https://x/a.jpg"


@pytest.mark.integration
@pytest.mark.asyncio
@respx.mock
async def test_theaudiodb_resolve_artwork_album_returns_thumb() -> None:
    payload = {"album": [{"idAlbum": "1", "strAlbum": "A", "strAlbumThumb": "https://x/alb.jpg"}]}
    respx.get(_ALBUM_URL).mock(return_value=httpx.Response(200, json=payload))
    async with httpx.AsyncClient() as client:
        adapter = TheAudioDBSearchAdapter(client=client)
        art = await adapter.resolve_artwork(ResultKind.ALBUM, "A", "Artist")
    assert art == "https://x/alb.jpg"


@pytest.mark.integration
@pytest.mark.asyncio
@respx.mock
async def test_theaudiodb_resolve_artwork_track_falls_back_to_album_thumb() -> None:
    payload = {
        "track": [
            {"idTrack": "1", "strTrack": "T", "strTrackThumb": None, "strAlbumThumb": "https://x/t.jpg"}
        ]
    }
    respx.get(_TRACK_URL).mock(return_value=httpx.Response(200, json=payload))
    async with httpx.AsyncClient() as client:
        adapter = TheAudioDBSearchAdapter(client=client)
        art = await adapter.resolve_artwork(ResultKind.TRACK, "T", "Artist")
    assert art == "https://x/t.jpg"


@pytest.mark.integration
@pytest.mark.asyncio
@respx.mock
async def test_theaudiodb_resolve_artwork_returns_none_on_null() -> None:
    respx.get(_SEARCH_URL).mock(return_value=httpx.Response(200, json={"artists": None}))
    async with httpx.AsyncClient() as client:
        adapter = TheAudioDBSearchAdapter(client=client)
        art = await adapter.resolve_artwork(ResultKind.ARTIST, "Nobody", None)
    assert art is None


@pytest.mark.integration
@pytest.mark.asyncio
@respx.mock
async def test_theaudiodb_resolve_artwork_returns_none_on_error() -> None:
    respx.get(_SEARCH_URL).mock(return_value=httpx.Response(500, text="boom"))
    async with httpx.AsyncClient() as client:
        adapter = TheAudioDBSearchAdapter(client=client)
        art = await adapter.resolve_artwork(ResultKind.ARTIST, "X", None)
    assert art is None


@pytest.mark.integration
@pytest.mark.asyncio
@respx.mock
async def test_theaudiodb_adapter_lookup_by_url_returns_none() -> None:
    async with httpx.AsyncClient() as client:
        adapter = TheAudioDBSearchAdapter(client=client)
        result = await adapter.lookup_by_url("https://www.theaudiodb.com/artist/111239")
    assert result is None

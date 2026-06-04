"""Unit tests for GetArtistTopTracks and GetArtistAlbums use cases."""

from __future__ import annotations

import pytest

from altune.application.discovery.get_artist_content import (
    GetArtistAlbums,
    GetArtistAlbumsInput,
    GetArtistTopTracks,
    GetArtistTopTracksInput,
)
from altune.application.discovery.ports import ArtistContentProvider, ContentFetchResponse
from altune.domain.discovery.confidence import Confidence
from altune.domain.discovery.provider_status import ProviderStatus
from altune.domain.discovery.result_kind import ResultKind
from altune.domain.discovery.search_result import SearchResult
from altune.domain.discovery.source_ref import SourceRef


class FakeArtistContentProvider:
    """In-memory artist content provider for testing."""

    def __init__(
        self,
        name: str,
        top_tracks: tuple[SearchResult, ...],
        albums: tuple[SearchResult, ...],
    ) -> None:
        self._name = name
        self._top_tracks = top_tracks
        self._albums = albums

    @property
    def name(self) -> str:
        return self._name

    async def get_artist_top_tracks(self, external_id: str, limit: int) -> ContentFetchResponse:
        return ContentFetchResponse(
            provider_name=self._name,
            status=ProviderStatus.OK,
            items=self._top_tracks[:limit],
            latency_ms=50,
        )

    async def get_artist_albums(self, external_id: str, limit: int) -> ContentFetchResponse:
        return ContentFetchResponse(
            provider_name=self._name,
            status=ProviderStatus.OK,
            items=self._albums[:limit],
            latency_ms=50,
        )


def _track(title: str, artist: str) -> SearchResult:
    return SearchResult(
        kind=ResultKind.TRACK,
        title=title,
        subtitle=artist,
        image_url=None,
        confidence=Confidence.HIGH,
        sources=(
            SourceRef(provider="deezer", external_id="123", url="https://deezer.com/track/123"),
        ),
        extras={},
    )


def _album(title: str, artist: str) -> SearchResult:
    return SearchResult(
        kind=ResultKind.ALBUM,
        title=title,
        subtitle=artist,
        image_url=None,
        confidence=Confidence.HIGH,
        sources=(
            SourceRef(provider="deezer", external_id="456", url="https://deezer.com/album/456"),
        ),
        extras={},
    )


@pytest.mark.asyncio
async def test_get_artist_top_tracks_returns_tracks() -> None:
    tracks = (_track("Hit Song", "Artist"), _track("Another Hit", "Artist"))
    provider = FakeArtistContentProvider("deezer", top_tracks=tracks, albums=())
    use_case = GetArtistTopTracks(providers={"deezer": provider})

    result = await use_case.execute(
        GetArtistTopTracksInput(provider="deezer", external_id="artist123")
    )

    assert result.status == ProviderStatus.OK
    assert len(result.items) == 2
    assert result.items[0].title == "Hit Song"


@pytest.mark.asyncio
async def test_get_artist_top_tracks_respects_limit() -> None:
    tracks = tuple(_track(f"Song {i}", "Artist") for i in range(10))
    provider = FakeArtistContentProvider("deezer", top_tracks=tracks, albums=())
    use_case = GetArtistTopTracks(providers={"deezer": provider})

    result = await use_case.execute(
        GetArtistTopTracksInput(provider="deezer", external_id="artist123", limit=5)
    )

    assert len(result.items) == 5


@pytest.mark.asyncio
async def test_get_artist_top_tracks_returns_error_for_unknown_provider() -> None:
    use_case = GetArtistTopTracks(providers={})

    result = await use_case.execute(
        GetArtistTopTracksInput(provider="unknown", external_id="artist123")
    )

    assert result.status == ProviderStatus.ERROR
    assert result.items == ()


@pytest.mark.asyncio
async def test_get_artist_albums_returns_albums() -> None:
    albums = (_album("Album One", "Artist"), _album("Album Two", "Artist"))
    provider = FakeArtistContentProvider("deezer", top_tracks=(), albums=albums)
    use_case = GetArtistAlbums(providers={"deezer": provider})

    result = await use_case.execute(
        GetArtistAlbumsInput(provider="deezer", external_id="artist123")
    )

    assert result.status == ProviderStatus.OK
    assert len(result.items) == 2
    assert result.items[0].title == "Album One"


@pytest.mark.asyncio
async def test_get_artist_albums_respects_limit() -> None:
    albums = tuple(_album(f"Album {i}", "Artist") for i in range(15))
    provider = FakeArtistContentProvider("deezer", top_tracks=(), albums=albums)
    use_case = GetArtistAlbums(providers={"deezer": provider})

    result = await use_case.execute(
        GetArtistAlbumsInput(provider="deezer", external_id="artist123", limit=10)
    )

    assert len(result.items) == 10


@pytest.mark.asyncio
async def test_get_artist_albums_returns_error_for_unknown_provider() -> None:
    use_case = GetArtistAlbums(providers={})

    result = await use_case.execute(
        GetArtistAlbumsInput(provider="unknown", external_id="artist123")
    )

    assert result.status == ProviderStatus.ERROR
    assert result.items == ()

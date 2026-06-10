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


def _album_with_source(
    title: str, artist: str, provider: str, ext_id: str, track_count: int = 10
) -> SearchResult:
    return SearchResult(
        kind=ResultKind.ALBUM,
        title=title,
        subtitle=artist,
        image_url=None,
        confidence=Confidence.HIGH,
        sources=(
            SourceRef(provider=provider, external_id=ext_id, url=f"https://{provider}/{ext_id}"),
        ),
        extras={"track_count": track_count},
    )


@pytest.mark.unit
def test_album_dedup_preserves_sources_from_all_providers() -> None:
    """AC#22: merging duplicate albums keeps both providers' source links."""
    from altune.application.discovery.get_artist_content import _dedup_albums

    dz_album = _album_with_source("Greatest Hits", "Artist", "deezer", "dz-456", track_count=12)
    mb_album = _album_with_source(
        "Greatest Hits", "Artist", "musicbrainz", "mb-789", track_count=10
    )

    deduped = _dedup_albums((dz_album, mb_album))
    assert len(deduped) == 1
    providers = {s.provider for s in deduped[0].sources}
    assert "deezer" in providers
    assert "musicbrainz" in providers


class _InMemoryValidationCache:
    def __init__(self) -> None:
        from altune.domain.discovery.content_validation_status import ContentValidationStatus

        self._data: dict[tuple[str, str], ContentValidationStatus] = {}
        self.calls: list[tuple[str, str, str]] = []

    async def get(self, provider: str, external_id: str) -> ContentValidationStatus:
        from altune.domain.discovery.content_validation_status import ContentValidationStatus

        return self._data.get((provider, external_id), ContentValidationStatus.UNKNOWN)

    async def record(
        self, provider: str, external_id: str, status: ContentValidationStatus
    ) -> None:
        self._data[(provider, external_id)] = status
        self.calls.append((provider, external_id, status.value))


class _InMemoryFetchSuccessStore:
    def __init__(self) -> None:
        self._data: dict[tuple[str, str], list[bool]] = {}

    async def get_rate(self, provider: str, external_id: str) -> float:
        history = self._data.get((provider, external_id), [])
        return sum(1 for x in history if x) / len(history) if history else 1.0

    async def record(self, provider: str, external_id: str, *, success: bool) -> None:
        self._data.setdefault((provider, external_id), []).append(success)


@pytest.mark.asyncio
async def test_artist_content_fetch_records_validation_failure() -> None:
    """AC#12: failed artist content fetch records UNFETCHABLE."""
    use_case = GetArtistTopTracks(
        providers={},
        content_validation_cache=_InMemoryValidationCache(),
        fetch_success_store=_InMemoryFetchSuccessStore(),
    )
    cache = use_case.content_validation_cache
    await use_case.execute(GetArtistTopTracksInput(provider="deezer", external_id="art123"))
    assert ("deezer", "art123", "unfetchable") in cache.calls

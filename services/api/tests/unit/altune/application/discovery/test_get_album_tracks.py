"""Unit tests for GetAlbumTracks use case."""

from __future__ import annotations

import pytest

from altune.application.discovery.get_album_tracks import GetAlbumTracks, GetAlbumTracksInput
from altune.application.discovery.ports import AlbumContentProvider, ContentFetchResponse
from altune.domain.discovery.provider_status import ProviderStatus
from altune.domain.discovery.search_result import SearchResult
from altune.domain.discovery.result_kind import ResultKind
from altune.domain.discovery.confidence import Confidence
from altune.domain.discovery.source_ref import SourceRef


class FakeAlbumContentProvider:
    """In-memory album content provider for testing."""

    def __init__(self, name: str, tracks: tuple[SearchResult, ...]) -> None:
        self._name = name
        self._tracks = tracks

    @property
    def name(self) -> str:
        return self._name

    async def get_album_tracks(self, external_id: str, limit: int) -> ContentFetchResponse:
        return ContentFetchResponse(
            provider_name=self._name,
            status=ProviderStatus.OK,
            items=self._tracks[:limit],
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


@pytest.mark.asyncio
async def test_get_album_tracks_returns_tracks_from_provider() -> None:
    tracks = (_track("Track 1", "Artist"), _track("Track 2", "Artist"))
    provider = FakeAlbumContentProvider("deezer", tracks)
    use_case = GetAlbumTracks(providers={"deezer": provider})

    result = await use_case.execute(GetAlbumTracksInput(provider="deezer", external_id="album123"))

    assert result.status == ProviderStatus.OK
    assert len(result.items) == 2
    assert result.items[0].title == "Track 1"


@pytest.mark.asyncio
async def test_get_album_tracks_respects_limit() -> None:
    tracks = (_track("Track 1", "Artist"), _track("Track 2", "Artist"), _track("Track 3", "Artist"))
    provider = FakeAlbumContentProvider("deezer", tracks)
    use_case = GetAlbumTracks(providers={"deezer": provider})

    result = await use_case.execute(
        GetAlbumTracksInput(provider="deezer", external_id="album123", limit=2)
    )

    assert len(result.items) == 2


@pytest.mark.asyncio
async def test_get_album_tracks_returns_error_for_unknown_provider() -> None:
    use_case = GetAlbumTracks(providers={})

    result = await use_case.execute(GetAlbumTracksInput(provider="unknown", external_id="album123"))

    assert result.status == ProviderStatus.ERROR
    assert result.items == ()
    assert result.provider_name == "unknown"

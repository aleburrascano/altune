"""Unit tests for GetAlbumTracks use case."""

from __future__ import annotations

import pytest

from altune.application.discovery.get_album_tracks import GetAlbumTracks, GetAlbumTracksInput
from altune.application.discovery.ports import ContentFetchResponse
from altune.domain.discovery.confidence import Confidence
from altune.domain.discovery.provider_status import ProviderStatus
from altune.domain.discovery.result_kind import ResultKind
from altune.domain.discovery.search_result import SearchResult
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


class FakeErrorAlbumProvider:
    """Provider that always returns an error."""

    def __init__(self, name: str) -> None:
        self._name = name

    @property
    def name(self) -> str:
        return self._name

    async def get_album_tracks(self, external_id: str, limit: int) -> ContentFetchResponse:
        return ContentFetchResponse(
            provider_name=self._name,
            status=ProviderStatus.ERROR,
            items=(),
            latency_ms=50,
        )


class InMemoryContentValidationCache:
    """In-memory fake for ContentValidationCache."""

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


class InMemoryFetchSuccessStore:
    """In-memory fake for FetchSuccessStore."""

    def __init__(self) -> None:
        self._data: dict[tuple[str, str], list[bool]] = {}

    async def get_rate(self, provider: str, external_id: str) -> float:
        history = self._data.get((provider, external_id), [])
        if not history:
            return 1.0
        return sum(1 for x in history if x) / len(history)

    async def record(self, provider: str, external_id: str, *, success: bool) -> None:
        key = (provider, external_id)
        if key not in self._data:
            self._data[key] = []
        self._data[key].append(success)


@pytest.mark.asyncio
async def test_album_tracks_fetch_records_validation_success() -> None:
    """AC#14: successful fetch records FETCHABLE in validation cache."""
    tracks = (_track("Track 1", "Artist"),)
    provider = FakeAlbumContentProvider("deezer", tracks)
    cache = InMemoryContentValidationCache()
    store = InMemoryFetchSuccessStore()
    use_case = GetAlbumTracks(
        providers={"deezer": provider},
        content_validation_cache=cache,
        fetch_success_store=store,
    )

    await use_case.execute(GetAlbumTracksInput(provider="deezer", external_id="album123"))

    assert ("deezer", "album123", "fetchable") in cache.calls
    assert await store.get_rate("deezer", "album123") == 1.0


@pytest.mark.asyncio
async def test_album_tracks_fetch_records_validation_failure() -> None:
    """AC#12: failed fetch records UNFETCHABLE + decreases success rate."""
    provider = FakeErrorAlbumProvider("deezer")
    cache = InMemoryContentValidationCache()
    store = InMemoryFetchSuccessStore()
    use_case = GetAlbumTracks(
        providers={"deezer": provider},
        content_validation_cache=cache,
        fetch_success_store=store,
    )

    await use_case.execute(GetAlbumTracksInput(provider="deezer", external_id="album123"))

    assert ("deezer", "album123", "unfetchable") in cache.calls
    assert await store.get_rate("deezer", "album123") == 0.0


@pytest.mark.asyncio
async def test_self_healing_score_recovers_after_success() -> None:
    """AC#13: after failures, a successful fetch recovers the success rate."""
    store = InMemoryFetchSuccessStore()
    await store.record("deezer", "album123", success=False)
    await store.record("deezer", "album123", success=False)

    assert await store.get_rate("deezer", "album123") == 0.0

    await store.record("deezer", "album123", success=True)
    rate = await store.get_rate("deezer", "album123")
    assert rate > 0.0
    assert abs(rate - 1 / 3) < 0.01

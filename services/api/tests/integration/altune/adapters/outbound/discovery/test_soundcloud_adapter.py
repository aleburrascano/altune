# mypy: warn_unused_ignores = False, disable_error_code = "no-any-return,untyped-decorator,type-arg"
"""SoundCloudSearchAdapter — slice 21 fake-extractor integration tests.

yt-dlp itself is sync + slow + network-dependent; the adapter takes an
async extractor callable so tests can inject the captured fixture
directly without invoking yt-dlp.
"""

from __future__ import annotations

import json
from pathlib import Path
from typing import TYPE_CHECKING, Any

import pytest

from altune.adapters.outbound.discovery.soundcloud.adapter import SoundCloudSearchAdapter
from altune.domain.discovery.provider import ProviderName
from altune.domain.discovery.provider_status import ProviderStatus
from altune.domain.discovery.result_kind import ResultKind

if TYPE_CHECKING:
    from collections.abc import Awaitable, Callable

_FIXTURE = (
    Path(__file__).resolve().parents[4]
    / "fixtures"
    / "discovery"
    / "soundcloud"
    / "track_search.json"
)


def _load_fixture() -> dict[str, Any]:
    return json.loads(_FIXTURE.read_text(encoding="utf-8"))


def _make_fixture_extractor(
    payload: dict[str, Any],
) -> Callable[[str], Awaitable[dict[str, Any]]]:
    async def _extract(query: str) -> dict[str, Any]:
        _ = query
        return payload

    return _extract


@pytest.mark.integration
@pytest.mark.asyncio
async def test_soundcloud_adapter_translates_scsearch_entries() -> None:
    payload = _load_fixture()
    adapter = SoundCloudSearchAdapter(extractor=_make_fixture_extractor(payload))
    resp = await adapter.search(
        "the beatles let it be", frozenset({ResultKind.TRACK}), limit=5
    )
    assert resp.provider_name == "soundcloud"
    assert resp.status is ProviderStatus.OK
    assert len(resp.results) == 5
    first = resp.results[0]
    assert first.kind is ResultKind.TRACK
    assert first.title == "Let It Be... Naked Podcast 1/5"
    assert first.subtitle == "thebeatles"
    assert first.sources[0].provider is ProviderName.SOUNDCLOUD
    assert first.sources[0].external_id == "260805563"
    assert first.extras["duration_seconds"] == 312
    assert first.extras["preview_url"] is None
    assert first.extras["isrc"] is None
    assert first.image_url is not None
    # The fixture's largest-by-width thumbnail is t500x500 (500px);
    # the 'original' entry has no width, only preference, so it's used
    # as a fallback when width is absent — not preferred over t500x500.
    assert "t500x500" in first.image_url


@pytest.mark.integration
@pytest.mark.asyncio
async def test_soundcloud_adapter_drops_malformed_entry() -> None:
    payload: dict[str, Any] = {
        "entries": [
            {"id": "1", "title": None, "uploader": "X", "webpage_url": "https://x/1"},
            {
                "id": "2",
                "title": "Good Track",
                "uploader": "Good Uploader",
                "webpage_url": "https://x/2",
            },
        ]
    }
    adapter = SoundCloudSearchAdapter(extractor=_make_fixture_extractor(payload))
    resp = await adapter.search("q", frozenset({ResultKind.TRACK}), limit=5)
    assert len(resp.results) == 1
    assert resp.results[0].title == "Good Track"


@pytest.mark.integration
@pytest.mark.asyncio
async def test_soundcloud_adapter_skips_none_entries() -> None:
    # yt-dlp's ignoreerrors=True can produce None entries when individual
    # tracks fail extraction. The adapter must skip them.
    payload: dict[str, Any] = {
        "entries": [
            None,
            {
                "id": "2",
                "title": "Survives",
                "uploader": "U",
                "webpage_url": "https://x/2",
            },
            None,
        ]
    }
    adapter = SoundCloudSearchAdapter(extractor=_make_fixture_extractor(payload))
    resp = await adapter.search("q", frozenset({ResultKind.TRACK}), limit=5)
    assert len(resp.results) == 1
    assert resp.results[0].title == "Survives"


@pytest.mark.integration
@pytest.mark.asyncio
async def test_soundcloud_adapter_maps_extractor_exception_to_error() -> None:
    async def _failing_extractor(query: str) -> dict[str, Any]:
        _ = query
        msg = "yt-dlp network failure"
        raise RuntimeError(msg)

    adapter = SoundCloudSearchAdapter(extractor=_failing_extractor)
    resp = await adapter.search("q", frozenset({ResultKind.TRACK}), limit=5)
    assert resp.status is ProviderStatus.ERROR
    assert resp.results == ()


@pytest.mark.integration
@pytest.mark.asyncio
async def test_soundcloud_adapter_returns_empty_for_non_track_kinds() -> None:
    async def _never_called(query: str) -> dict[str, Any]:
        _ = query
        raise AssertionError("extractor should not be called for non-track kinds")

    adapter = SoundCloudSearchAdapter(extractor=_never_called)
    resp = await adapter.search(
        "q", frozenset({ResultKind.ARTIST, ResultKind.ALBUM}), limit=5
    )
    assert resp.status is ProviderStatus.OK
    assert resp.results == ()


@pytest.mark.integration
@pytest.mark.asyncio
async def test_soundcloud_adapter_constructs_scsearch_query() -> None:
    captured: list[str] = []

    async def _capture_extractor(query: str) -> dict[str, Any]:
        captured.append(query)
        return {"entries": []}

    adapter = SoundCloudSearchAdapter(extractor=_capture_extractor)
    await adapter.search("beatles", frozenset({ResultKind.TRACK}), limit=7)
    assert captured == ["scsearch7:beatles"]

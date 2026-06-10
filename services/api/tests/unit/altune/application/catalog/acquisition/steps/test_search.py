"""SearchStep — tiered waterfall tests."""

from __future__ import annotations

from datetime import UTC, datetime
from pathlib import Path
from uuid import UUID

import pytest

from altune.application.catalog.acquisition.context import AcquisitionContext
from altune.application.catalog.acquisition.steps.search import SearchStep
from altune.application.catalog.ports import AudioCandidate
from altune.domain.catalog.track import Track
from altune.domain.catalog.track_id import TrackId
from altune.domain.shared.user_id import UserId

_TID = TrackId(UUID("11111111-1111-1111-1111-111111111111"))
_UID = UserId(UUID("00000000-0000-0000-0000-000000000001"))


def _track(*, isrc: str | None = None, album: str | None = None) -> Track:
    return Track(
        id=_TID,
        user_id=_UID,
        title="Blinding Lights",
        artist="The Weeknd",
        album=album,
        duration_seconds=200,
        added_at=datetime(2026, 1, 1, tzinfo=UTC),
        isrc=isrc,
    )


class FakeSearcher:
    def __init__(self, responses: dict[str, list[AudioCandidate]]) -> None:
        self._responses = responses
        self.queries: list[str] = []

    async def search(self, query: str, limit: int = 5) -> list[AudioCandidate]:
        self.queries.append(query)
        return self._responses.get(query, [])

    async def download(self, url: str, temp_dir: Path) -> Path:
        return temp_dir / "test.mp3"


_GOOD = AudioCandidate(
    title="Blinding Lights",
    artist="The Weeknd",
    duration_seconds=200,
    url="http://good",
)


@pytest.mark.unit
async def test_search_tries_isrc_tier_first() -> None:
    searcher = FakeSearcher({"US1234567890": [_GOOD]})
    step = SearchStep(searcher)
    ctx = AcquisitionContext(track=_track(isrc="US1234567890"))

    await step.execute(ctx)

    assert searcher.queries[0] == "US1234567890"
    assert len(ctx.candidates) > 0


@pytest.mark.unit
async def test_search_falls_through_on_empty_tier() -> None:
    searcher = FakeSearcher(
        {
            "Blinding Lights The Weeknd": [_GOOD],
        }
    )
    step = SearchStep(searcher)
    ctx = AcquisitionContext(track=_track())

    await step.execute(ctx)

    assert "Blinding Lights The Weeknd" in searcher.queries


@pytest.mark.unit
async def test_search_includes_album_tier() -> None:
    searcher = FakeSearcher(
        {
            "Blinding Lights The Weeknd After Hours": [_GOOD],
        }
    )
    step = SearchStep(searcher)
    ctx = AcquisitionContext(track=_track(album="After Hours"))

    await step.execute(ctx)

    assert any("After Hours" in q for q in searcher.queries)

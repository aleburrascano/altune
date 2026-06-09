"""SelectStep — gate application + candidate selection tests."""

from __future__ import annotations

from datetime import UTC, datetime
from uuid import UUID

import pytest

from altune.application.catalog.acquisition.context import AcquisitionContext
from altune.application.catalog.acquisition.steps.select import NoMatchFoundError, SelectStep
from altune.application.catalog.ports import AudioCandidate
from altune.domain.catalog.track import Track
from altune.domain.catalog.track_id import TrackId
from altune.domain.shared.user_id import UserId

_TID = TrackId(UUID("11111111-1111-1111-1111-111111111111"))
_UID = UserId(UUID("00000000-0000-0000-0000-000000000001"))


def _track() -> Track:
    return Track(
        id=_TID,
        user_id=_UID,
        title="Blinding Lights",
        artist="The Weeknd",
        album=None,
        duration_seconds=200,
        added_at=datetime(2026, 1, 1, tzinfo=UTC),
    )


@pytest.mark.unit
async def test_select_step_picks_best_passing_candidate() -> None:
    good = AudioCandidate(
        title="Blinding Lights", artist="The Weeknd", duration_seconds=200, url="http://good"
    )
    ctx = AcquisitionContext(track=_track(), candidates=[good])
    step = SelectStep()

    result = await step.execute(ctx)

    assert result.selected is not None
    assert result.selected.url == "http://good"


@pytest.mark.unit
async def test_select_step_raises_no_match_when_all_fail() -> None:
    bad = AudioCandidate(
        title="Bohemian Rhapsody", artist="Queen", duration_seconds=354, url="http://bad"
    )
    ctx = AcquisitionContext(track=_track(), candidates=[bad])
    step = SelectStep()

    with pytest.raises(NoMatchFoundError, match="no_match_found"):
        await step.execute(ctx)


@pytest.mark.unit
async def test_select_step_raises_on_empty_candidates() -> None:
    ctx = AcquisitionContext(track=_track(), candidates=[])
    step = SelectStep()

    with pytest.raises(NoMatchFoundError):
        await step.execute(ctx)

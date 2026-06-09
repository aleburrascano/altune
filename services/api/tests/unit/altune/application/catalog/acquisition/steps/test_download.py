"""DownloadStep tests."""

from __future__ import annotations

from datetime import UTC, datetime
from pathlib import Path
from uuid import UUID

import pytest

from altune.application.catalog.acquisition.context import AcquisitionContext
from altune.application.catalog.acquisition.steps.download import DownloadStep
from altune.application.catalog.ports import AudioCandidate
from altune.domain.catalog.track import Track
from altune.domain.catalog.track_id import TrackId
from altune.domain.shared.user_id import UserId

_TID = TrackId(UUID("11111111-1111-1111-1111-111111111111"))
_UID = UserId(UUID("00000000-0000-0000-0000-000000000001"))


def _track(duration: int | None = 200) -> Track:
    return Track(
        id=_TID, user_id=_UID, title="Song", artist="Artist",
        album=None, duration_seconds=duration,
        added_at=datetime(2026, 1, 1, tzinfo=UTC),
    )


class FakeSearcher:
    async def search(self, query: str, limit: int = 5) -> list[AudioCandidate]:
        return []

    async def download(self, url: str, temp_dir: Path) -> Path:
        out = temp_dir / "audio.mp3"
        out.write_bytes(b"\x00" * 100)
        return out


@pytest.mark.unit
async def test_download_step_populates_temp_path() -> None:
    searcher = FakeSearcher()
    step = DownloadStep(searcher)
    candidate = AudioCandidate(title="Song", artist="Artist", duration_seconds=200, url="http://x")
    ctx = AcquisitionContext(track=_track(), selected=candidate)

    result = await step.execute(ctx)

    assert result.temp_path is not None
    assert result.temp_path.exists()
    result.temp_path.unlink()
    result.temp_path.parent.rmdir()


@pytest.mark.unit
async def test_download_step_rollback_cleans_temp() -> None:
    searcher = FakeSearcher()
    step = DownloadStep(searcher)
    candidate = AudioCandidate(title="Song", artist="Artist", duration_seconds=200, url="http://x")
    ctx = AcquisitionContext(track=_track(), selected=candidate)

    await step.execute(ctx)
    assert ctx.temp_path is not None
    temp = ctx.temp_path

    await step.rollback(ctx)

    assert not temp.exists()

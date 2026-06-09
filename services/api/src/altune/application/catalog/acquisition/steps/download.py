"""DownloadStep — download audio via AudioSearcher port + duration check."""

from __future__ import annotations

import tempfile
from pathlib import Path
from typing import TYPE_CHECKING

import structlog

if TYPE_CHECKING:
    from altune.application.catalog.acquisition.context import AcquisitionContext
    from altune.application.catalog.ports import AudioSearcher

_logger = structlog.get_logger(__name__)
_DURATION_TOLERANCE = 15


class DownloadStep:
    def __init__(self, searcher: AudioSearcher) -> None:
        self._searcher = searcher

    async def execute(self, ctx: AcquisitionContext) -> AcquisitionContext:
        assert ctx.selected is not None
        temp_dir = Path(tempfile.mkdtemp(prefix="altune_acquire_"))
        path = await self._searcher.download(ctx.selected.url, temp_dir)
        ctx.temp_path = path
        self._check_duration(ctx)
        return ctx

    def _check_duration(self, ctx: AcquisitionContext) -> None:
        assert ctx.track is not None
        expected = ctx.track.duration_seconds
        actual = ctx.selected.duration_seconds if ctx.selected else None
        if expected is None or actual is None:
            return
        diff = abs(expected - actual)
        if diff > _DURATION_TOLERANCE:
            _logger.warning(
                "duration_mismatch",
                track_id=str(ctx.track.id),
                expected=expected,
                actual=actual,
                diff=diff,
            )

    async def rollback(self, ctx: AcquisitionContext) -> None:
        if ctx.temp_path and ctx.temp_path.exists():
            ctx.temp_path.unlink(missing_ok=True)
            parent = ctx.temp_path.parent
            if parent.exists() and not any(parent.iterdir()):
                parent.rmdir()

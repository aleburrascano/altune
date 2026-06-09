"""UpdateTrackStep — transition Track to READY with audio_ref."""

from __future__ import annotations

from dataclasses import replace
from typing import TYPE_CHECKING

from altune.domain.catalog.acquisition_status import AcquisitionStatus

if TYPE_CHECKING:
    from altune.application.catalog.acquisition.context import AcquisitionContext
    from altune.application.catalog.ports import TrackRepository


class UpdateTrackStep:
    def __init__(self, tracks: TrackRepository) -> None:
        self._tracks = tracks

    async def execute(self, ctx: AcquisitionContext) -> AcquisitionContext:
        assert ctx.track is not None
        assert ctx.audio_ref is not None
        updated = replace(
            ctx.track,
            audio_ref=ctx.audio_ref,
            acquisition_status=AcquisitionStatus.READY,
        )
        await self._tracks.update(updated)
        ctx.track = updated
        return ctx

    async def rollback(self, ctx: AcquisitionContext) -> None:
        if ctx.track and ctx.track.acquisition_status is AcquisitionStatus.READY:
            reverted = replace(
                ctx.track,
                audio_ref=None,
                acquisition_status=AcquisitionStatus.PENDING,
            )
            await self._tracks.update(reverted)
            ctx.track = reverted

"""SelectStep — apply gate matching, pick the best candidate."""

from __future__ import annotations

from typing import TYPE_CHECKING

from altune.application.catalog.acquisition.matching import select_best_candidate

if TYPE_CHECKING:
    from altune.application.catalog.acquisition.context import AcquisitionContext


class NoMatchFoundError(Exception):
    """All tiers exhausted, no candidate passed gates."""


class SelectStep:
    async def execute(self, ctx: AcquisitionContext) -> AcquisitionContext:
        assert ctx.track is not None
        track = ctx.track
        best = select_best_candidate(
            track_title=track.title,
            track_artist=track.artist,
            track_duration=track.duration_seconds,
            candidates=ctx.candidates,
        )
        if best is None:
            raise NoMatchFoundError("no_match_found")
        ctx.selected = best
        return ctx

    async def rollback(self, ctx: AcquisitionContext) -> None:
        pass

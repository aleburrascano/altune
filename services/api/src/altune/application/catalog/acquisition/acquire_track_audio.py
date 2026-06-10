"""AcquireTrackAudio — orchestrates the 6-step acquisition pipeline.

Triggered by the HTTP adapter after a new track is saved. Loads the track,
runs the pipeline, handles failure (sets FAILED + reason), and cleans up
temporary files. Skips if the track is already READY.
"""

from __future__ import annotations

import shutil
from dataclasses import replace
from typing import TYPE_CHECKING

import structlog

from altune.application.catalog.acquisition.context import AcquisitionContext
from altune.application.catalog.acquisition.pipeline import AcquisitionPipeline
from altune.application.catalog.acquisition.steps.download import DownloadStep
from altune.application.catalog.acquisition.steps.search import SearchStep
from altune.application.catalog.acquisition.steps.select import SelectStep
from altune.application.catalog.acquisition.steps.store import StoreStep
from altune.application.catalog.acquisition.steps.tag import TagStep
from altune.application.catalog.acquisition.steps.update_track import UpdateTrackStep
from altune.domain.catalog.acquisition_status import AcquisitionStatus

if TYPE_CHECKING:
    from altune.application.catalog.ports import AudioSearcher, AudioStore, TrackRepository
    from altune.domain.catalog.track_id import TrackId
    from altune.domain.shared.user_id import UserId

_logger = structlog.get_logger(__name__)


class AcquireTrackAudio:
    def __init__(
        self,
        tracks: TrackRepository,
        searcher: AudioSearcher,
        store: AudioStore,
    ) -> None:
        self._tracks = tracks
        self._searcher = searcher
        self._store = store

    async def execute(self, track_id: TrackId, user_id: UserId) -> None:
        track = await self._tracks.get_by_id(track_id, user_id)
        if track is None:
            _logger.warning("acquire_track_not_found", track_id=str(track_id))
            return
        if track.acquisition_status is AcquisitionStatus.READY:
            if track.audio_ref and self._store.exists(track.audio_ref):
                _logger.info("acquire_skip_already_ready", track_id=str(track_id))
                return
            _logger.info(
                "acquire_reacquire_missing_file", track_id=str(track_id), audio_ref=track.audio_ref
            )
            track = replace(track, acquisition_status=AcquisitionStatus.PENDING, audio_ref=None)
            await self._tracks.update(track)
        if track.acquisition_status is AcquisitionStatus.FAILED:
            _logger.info("acquire_retrying_failed", track_id=str(track_id))
            track = replace(
                track, acquisition_status=AcquisitionStatus.PENDING, failure_reason=None
            )
            await self._tracks.update(track)

        _logger.info(
            "track_acquisition_started",
            track_id=str(track_id),
            user_id=str(user_id),
            has_isrc=track.isrc is not None,
        )

        ctx = AcquisitionContext(track=track)
        pipeline = AcquisitionPipeline(
            [
                SearchStep(self._searcher),
                SelectStep(),
                DownloadStep(self._searcher),
                TagStep(),
                StoreStep(self._store),
                UpdateTrackStep(self._tracks),
            ]
        )

        try:
            ctx = await pipeline.run(ctx)
            _logger.info(
                "track_acquisition_completed",
                track_id=str(track_id),
                user_id=str(user_id),
                audio_ref=ctx.audio_ref,
            )
        except Exception as exc:
            reason = str(exc) or type(exc).__name__
            _logger.warning(
                "track_acquisition_failed",
                track_id=str(track_id),
                user_id=str(user_id),
                reason=reason,
            )
            await self._mark_failed(track_id, user_id, reason)
        finally:
            self._cleanup_temp(ctx)

    async def _mark_failed(self, track_id: TrackId, user_id: UserId, reason: str) -> None:
        track = await self._tracks.get_by_id(track_id, user_id)
        if track is None:
            return
        failed = replace(
            track,
            acquisition_status=AcquisitionStatus.FAILED,
            failure_reason=reason,
        )
        await self._tracks.update(failed)

    def _cleanup_temp(self, ctx: AcquisitionContext) -> None:
        if ctx.temp_path and ctx.temp_path.exists():
            parent = ctx.temp_path.parent
            try:
                shutil.rmtree(parent)
            except OSError:
                _logger.warning("temp_cleanup_failed", path=str(parent), exc_info=True)

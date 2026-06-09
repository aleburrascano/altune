"""TagStep — write ID3v2.4 tags to the downloaded MP3."""

from __future__ import annotations

from typing import TYPE_CHECKING

import structlog

if TYPE_CHECKING:
    from altune.application.catalog.acquisition.context import AcquisitionContext

_logger = structlog.get_logger(__name__)


class TagStep:
    async def execute(self, ctx: AcquisitionContext) -> AcquisitionContext:
        assert ctx.temp_path is not None
        assert ctx.track is not None
        track = ctx.track
        try:
            from mutagen.id3 import ID3, TALB, TCON, TDRC, TIT2, TPE1, TPE2, TRCK
            from mutagen.id3 import ID3NoHeaderError

            try:
                tags = ID3(ctx.temp_path)
            except ID3NoHeaderError:
                tags = ID3()
            tags.add(TIT2(encoding=3, text=[track.title]))
            tags.add(TPE1(encoding=3, text=[track.artist]))
            if track.album:
                tags.add(TALB(encoding=3, text=[track.album]))
            if track.year:
                tags.add(TDRC(encoding=3, text=[str(track.year)]))
            if track.track_number:
                tags.add(TRCK(encoding=3, text=[str(track.track_number)]))
            if track.album_artist:
                tags.add(TPE2(encoding=3, text=[track.album_artist]))
            if track.genre:
                tags.add(TCON(encoding=3, text=[track.genre]))
            tags.save(ctx.temp_path)
            _logger.info("id3_tags_written", track_id=str(track.id))
        except Exception:
            _logger.warning("id3_tagging_failed", track_id=str(track.id), exc_info=True)
        return ctx

    async def rollback(self, ctx: AcquisitionContext) -> None:
        pass

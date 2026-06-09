"""StoreStep — move file to permanent storage via AudioStore port."""

from __future__ import annotations

from typing import TYPE_CHECKING

if TYPE_CHECKING:
    from altune.application.catalog.acquisition.context import AcquisitionContext
    from altune.application.catalog.ports import AudioStore


class StoreStep:
    def __init__(self, store: AudioStore) -> None:
        self._store = store

    async def execute(self, ctx: AcquisitionContext) -> AcquisitionContext:
        assert ctx.temp_path is not None
        assert ctx.track is not None
        track = ctx.track
        ref = _build_audio_ref(track)
        await self._store.store(ctx.temp_path, ref)
        ctx.audio_ref = ref
        return ctx

    async def rollback(self, ctx: AcquisitionContext) -> None:
        pass


def _build_audio_ref(track: object) -> str:
    from altune.domain.catalog.track import Track

    assert isinstance(track, Track)
    parts = [
        str(track.user_id.value),
        _sanitize(track.artist),
        _sanitize(track.album or "Unknown Album"),
        _sanitize(track.title) + ".mp3",
    ]
    return "/".join(parts)


def _sanitize(name: str) -> str:
    forbidden = '<>:"/\\|?*;'
    result = name
    for ch in forbidden:
        result = result.replace(ch, "")
    return " ".join(result.split()).strip() or "Unknown"

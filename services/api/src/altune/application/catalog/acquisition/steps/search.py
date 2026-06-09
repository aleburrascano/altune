"""SearchStep — 4-tier waterfall via AudioSearcher port."""

from __future__ import annotations

from typing import TYPE_CHECKING

if TYPE_CHECKING:
    from altune.application.catalog.acquisition.context import AcquisitionContext
    from altune.application.catalog.ports import AudioSearcher


class SearchStep:
    def __init__(self, searcher: AudioSearcher) -> None:
        self._searcher = searcher

    async def execute(self, ctx: AcquisitionContext) -> AcquisitionContext:
        assert ctx.track is not None
        track = ctx.track
        tiers = self._build_tiers(track)
        seen_urls: set[str] = set()
        for query in tiers:
            candidates = await self._searcher.search(query, limit=5)
            for c in candidates:
                if c.url not in seen_urls:
                    ctx.candidates.append(c)
                    seen_urls.add(c.url)
        return ctx

    def _build_tiers(self, track: object) -> list[str]:
        from altune.domain.catalog.track import Track

        assert isinstance(track, Track)
        tiers: list[str] = []
        if track.isrc:
            tiers.append(f"{track.isrc}")
        tiers.append(f"{track.title} {track.artist}")
        if track.album:
            tiers.append(f"{track.title} {track.artist} {track.album}")
        tiers.append(f"{track.title} {track.artist} audio")
        return tiers

    async def rollback(self, ctx: AcquisitionContext) -> None:
        pass

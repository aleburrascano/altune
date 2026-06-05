from __future__ import annotations

from dataclasses import dataclass
from typing import TYPE_CHECKING

if TYPE_CHECKING:
    from collections.abc import Sequence

    from altune.application.catalog.ports import PlaylistRepository
    from altune.domain.catalog.playlist_id import PlaylistId
    from altune.domain.catalog.track_id import TrackId
    from altune.domain.shared.user_id import UserId


@dataclass(frozen=True)
class ReorderPlaylistTracksInput:
    playlist_id: PlaylistId
    user_id: UserId
    track_ids: Sequence[TrackId]


class ReorderPlaylistTracks:
    def __init__(self, playlists: PlaylistRepository) -> None:
        self._playlists = playlists

    async def execute(self, input: ReorderPlaylistTracksInput) -> bool:
        return await self._playlists.reorder_tracks(
            input.playlist_id,
            input.user_id,
            input.track_ids,
        )

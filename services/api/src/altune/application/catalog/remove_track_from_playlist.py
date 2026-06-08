from __future__ import annotations

from dataclasses import dataclass
from datetime import UTC, datetime
from typing import TYPE_CHECKING

import structlog

from altune.domain.catalog.events import TrackRemovedFromPlaylist

if TYPE_CHECKING:
    from altune.application.catalog.ports import PlaylistRepository
    from altune.domain.catalog.playlist_id import PlaylistId
    from altune.domain.catalog.track_id import TrackId
    from altune.domain.shared.user_id import UserId

_logger = structlog.get_logger(__name__)


@dataclass(frozen=True)
class RemoveTrackFromPlaylistInput:
    playlist_id: PlaylistId
    user_id: UserId
    track_id: TrackId


class RemoveTrackFromPlaylist:
    def __init__(self, playlists: PlaylistRepository) -> None:
        self._playlists = playlists

    async def execute(self, input: RemoveTrackFromPlaylistInput) -> bool:
        removed = await self._playlists.remove_track(
            input.playlist_id, input.user_id, input.track_id
        )
        if removed:
            event = TrackRemovedFromPlaylist(
                playlist_id=input.playlist_id,
                track_id=input.track_id,
                user_id=input.user_id,
                occurred_at=datetime.now(UTC),
            )
            _logger.info(
                "track_removed_from_playlist",
                playlist_id=str(event.playlist_id),
                track_id=str(event.track_id),
            )
        return removed

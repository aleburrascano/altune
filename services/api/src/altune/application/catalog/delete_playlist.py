from __future__ import annotations

from dataclasses import dataclass
from datetime import UTC, datetime
from typing import TYPE_CHECKING

import structlog

from altune.domain.catalog.events import PlaylistDeleted

if TYPE_CHECKING:
    from altune.application.catalog.ports import PlaylistRepository
    from altune.domain.catalog.playlist_id import PlaylistId
    from altune.domain.shared.user_id import UserId

_logger = structlog.get_logger(__name__)


@dataclass(frozen=True)
class DeletePlaylistInput:
    playlist_id: PlaylistId
    user_id: UserId


class DeletePlaylist:
    def __init__(self, playlists: PlaylistRepository) -> None:
        self._playlists = playlists

    async def execute(self, input: DeletePlaylistInput) -> bool:
        deleted = await self._playlists.delete(input.playlist_id, input.user_id)
        if deleted:
            event = PlaylistDeleted(
                playlist_id=input.playlist_id,
                user_id=input.user_id,
                occurred_at=datetime.now(UTC),
            )
            _logger.info(
                "playlist_deleted", playlist_id=str(event.playlist_id), user_id=str(event.user_id)
            )
        return deleted

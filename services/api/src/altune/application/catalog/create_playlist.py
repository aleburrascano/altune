from __future__ import annotations

from dataclasses import dataclass
from datetime import UTC, datetime
from typing import TYPE_CHECKING
from uuid import uuid4

import structlog

from altune.domain.catalog.events import PlaylistCreated
from altune.domain.catalog.playlist import Playlist
from altune.domain.catalog.playlist_id import PlaylistId

if TYPE_CHECKING:
    from altune.application.catalog.ports import PlaylistRepository
    from altune.domain.shared.user_id import UserId

_logger = structlog.get_logger(__name__)


@dataclass(frozen=True)
class CreatePlaylistInput:
    user_id: UserId
    name: str


class CreatePlaylist:
    def __init__(self, playlists: PlaylistRepository) -> None:
        self._playlists = playlists

    async def execute(self, input: CreatePlaylistInput) -> Playlist:
        now = datetime.now(UTC)
        playlist = Playlist(
            id=PlaylistId(uuid4()),
            user_id=input.user_id,
            name=input.name,
            created_at=now,
            updated_at=now,
        )
        persisted = await self._playlists.create(playlist)
        event = PlaylistCreated(
            playlist_id=persisted.id,
            user_id=persisted.user_id,
            occurred_at=now,
        )
        _logger.info(
            "playlist_created", playlist_id=str(event.playlist_id), user_id=str(event.user_id)
        )
        return persisted

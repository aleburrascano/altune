from __future__ import annotations

from dataclasses import dataclass
from typing import TYPE_CHECKING

if TYPE_CHECKING:
    from altune.application.catalog.ports import PlaylistRepository
    from altune.domain.catalog.playlist import Playlist
    from altune.domain.catalog.playlist_id import PlaylistId
    from altune.domain.shared.user_id import UserId


@dataclass(frozen=True)
class RenamePlaylistInput:
    playlist_id: PlaylistId
    user_id: UserId
    name: str


class RenamePlaylist:
    def __init__(self, playlists: PlaylistRepository) -> None:
        self._playlists = playlists

    async def execute(self, input: RenamePlaylistInput) -> Playlist | None:
        return await self._playlists.update_name(input.playlist_id, input.user_id, input.name)

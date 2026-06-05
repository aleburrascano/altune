from __future__ import annotations

from dataclasses import dataclass
from typing import TYPE_CHECKING

if TYPE_CHECKING:
    from collections.abc import Sequence

    from altune.application.catalog.ports import PlaylistRepository
    from altune.domain.catalog.playlist import Playlist
    from altune.domain.shared.user_id import UserId


@dataclass(frozen=True)
class ListPlaylistsInput:
    user_id: UserId


@dataclass(frozen=True)
class ListPlaylistsOutput:
    items: Sequence[Playlist]


class ListPlaylists:
    def __init__(self, playlists: PlaylistRepository) -> None:
        self._playlists = playlists

    async def execute(self, input: ListPlaylistsInput) -> ListPlaylistsOutput:
        items = await self._playlists.list_for_user(input.user_id)
        return ListPlaylistsOutput(items=items)

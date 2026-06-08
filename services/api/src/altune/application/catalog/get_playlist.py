from __future__ import annotations

from dataclasses import dataclass
from typing import TYPE_CHECKING

if TYPE_CHECKING:
    from collections.abc import Sequence

    from altune.application.catalog.ports import PlaylistRepository
    from altune.domain.catalog.playlist import Playlist
    from altune.domain.catalog.playlist_id import PlaylistId
    from altune.domain.catalog.track import Track
    from altune.domain.shared.user_id import UserId


@dataclass(frozen=True)
class GetPlaylistInput:
    playlist_id: PlaylistId
    user_id: UserId


@dataclass(frozen=True)
class GetPlaylistOutput:
    playlist: Playlist
    tracks: Sequence[Track]


class GetPlaylist:
    def __init__(self, playlists: PlaylistRepository) -> None:
        self._playlists = playlists

    async def execute(self, input: GetPlaylistInput) -> GetPlaylistOutput | None:
        result = await self._playlists.get_with_tracks(input.playlist_id, input.user_id)
        if result is None:
            return None
        playlist, tracks = result
        return GetPlaylistOutput(playlist=playlist, tracks=tracks)

"""ResultKind — discoverable music shape.

Three members: artist, album, track. Playlists were removed in
discover-music-v2 (the user dropped playlist scope entirely).
"""

from __future__ import annotations

from enum import Enum


class ResultKind(Enum):
    """Kind of discovered result; wire-serialized as lowercase string."""

    ARTIST = "artist"
    ALBUM = "album"
    TRACK = "track"

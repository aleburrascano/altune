"""ResultKind — discoverable music shape per AC#1 wire contract.

Four members: artist, album, track, playlist. Per ADR-0007's locked
result-type breadth.
"""

from __future__ import annotations

from enum import Enum


class ResultKind(Enum):
    """Kind of discovered result; wire-serialized as lowercase string."""

    ARTIST = "artist"
    ALBUM = "album"
    TRACK = "track"
    PLAYLIST = "playlist"

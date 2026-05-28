"""ProviderName — the four locked sources per ADR-0007.

Deezer, MusicBrainz, SoundCloud (via yt-dlp), Last.fm.
"""

from __future__ import annotations

from enum import Enum


class ProviderName(Enum):
    """One of the four locked discovery sources."""

    DEEZER = "deezer"
    MUSICBRAINZ = "musicbrainz"
    SOUNDCLOUD = "soundcloud"
    LASTFM = "lastfm"

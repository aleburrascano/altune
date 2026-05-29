"""ProviderName — the discovery sources.

Deezer, MusicBrainz, SoundCloud (via yt-dlp), Last.fm per ADR-0007;
iTunes added by the ranking-overhaul addendum (free no-auth Search API).
"""

from __future__ import annotations

from enum import Enum


class ProviderName(Enum):
    """One of the discovery sources."""

    DEEZER = "deezer"
    MUSICBRAINZ = "musicbrainz"
    SOUNDCLOUD = "soundcloud"
    LASTFM = "lastfm"
    ITUNES = "itunes"

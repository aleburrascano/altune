"""url_router — slice 32. Map a pasted URL to its provider.

Returns None for non-URL input or unsupported hosts; SearchMusic uses
that to either short-circuit (URL maps to one of our providers) or
fall through to text search (AC#10a).
"""

from __future__ import annotations

import re

from altune.domain.discovery.provider import ProviderName

_URL_PATTERNS: tuple[tuple[re.Pattern[str], ProviderName], ...] = (
    (re.compile(r"^https?://(www\.)?deezer\.com/", re.IGNORECASE), ProviderName.DEEZER),
    (
        re.compile(r"^https?://(www\.)?musicbrainz\.org/", re.IGNORECASE),
        ProviderName.MUSICBRAINZ,
    ),
    (
        re.compile(r"^https?://(www\.)?soundcloud\.com/", re.IGNORECASE),
        ProviderName.SOUNDCLOUD,
    ),
    (re.compile(r"^https?://(www\.)?last\.fm/", re.IGNORECASE), ProviderName.LASTFM),
)


def match_provider(query: str) -> ProviderName | None:
    """Return the ProviderName whose URL pattern matches `query`, or None."""
    stripped = query.strip()
    for pattern, provider in _URL_PATTERNS:
        if pattern.match(stripped):
            return provider
    return None


def is_url_like(query: str) -> bool:
    """True if query starts with http(s):// — used for the fall-through branch."""
    stripped = query.strip().lower()
    return stripped.startswith(("http://", "https://"))

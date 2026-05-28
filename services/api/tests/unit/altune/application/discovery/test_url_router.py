"""url_router — slice 32 tests."""

from __future__ import annotations

import pytest

from altune.application.discovery.url_router import is_url_like, match_provider
from altune.domain.discovery.provider import ProviderName


@pytest.mark.unit
@pytest.mark.parametrize(
    ("url", "expected"),
    [
        ("https://deezer.com/track/123", ProviderName.DEEZER),
        ("https://www.deezer.com/track/123", ProviderName.DEEZER),
        ("http://deezer.com/album/456", ProviderName.DEEZER),
        ("https://musicbrainz.org/recording/abc", ProviderName.MUSICBRAINZ),
        ("https://soundcloud.com/artist/track", ProviderName.SOUNDCLOUD),
        ("https://www.soundcloud.com/leak/123", ProviderName.SOUNDCLOUD),
        ("https://last.fm/music/Artist/_/Track", ProviderName.LASTFM),
        ("https://www.last.fm/music/Artist", ProviderName.LASTFM),
    ],
)
def test_url_router_matches_supported_hosts(url: str, expected: ProviderName) -> None:
    assert match_provider(url) is expected


@pytest.mark.unit
@pytest.mark.parametrize(
    "url",
    [
        "https://spotify.com/track/123",  # explicit out-of-scope
        "https://open.spotify.com/track/123",
        "https://youtube.com/watch?v=x",
        "https://music.apple.com/album/123",
        "https://example.com",
        "not a url",
        "",
        "   ",
    ],
)
def test_url_router_returns_none_for_unsupported_or_non_url(url: str) -> None:
    assert match_provider(url) is None


@pytest.mark.unit
@pytest.mark.parametrize(
    ("query", "expected"),
    [
        ("https://example.com", True),
        ("http://example.com", True),
        ("HTTP://EXAMPLE.COM", True),
        ("the beatles", False),
        ("", False),
    ],
)
def test_is_url_like(query: str, expected: bool) -> None:
    assert is_url_like(query) is expected

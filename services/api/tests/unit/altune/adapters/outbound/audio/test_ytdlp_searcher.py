"""YtDlpAudioSearcher — opts injection for cookiefile and ignoreerrors."""

from __future__ import annotations

from pathlib import Path
from unittest.mock import MagicMock, patch

import pytest

from altune.adapters.outbound.audio.ytdlp_searcher import YtDlpAudioSearcher


def _make_mock_ydl(entries: list[dict[str, object]] | None = None) -> MagicMock:
    """Return a MagicMock that behaves like a yt_dlp.YoutubeDL context manager."""
    mock_ydl = MagicMock()
    mock_ydl.__enter__ = MagicMock(return_value=mock_ydl)
    mock_ydl.__exit__ = MagicMock(return_value=False)
    mock_ydl.extract_info.return_value = {
        "entries": entries or [],
    }
    return mock_ydl


@pytest.mark.unit
def test_search_opts_include_cookiefile_when_configured() -> None:
    searcher = YtDlpAudioSearcher(cookie_file="/tmp/cookies.txt")
    mock_ydl = _make_mock_ydl()
    with patch("yt_dlp.YoutubeDL", return_value=mock_ydl) as mock_cls:
        searcher._search_sync("test query", 5)
    opts = mock_cls.call_args[0][0]
    assert opts["cookiefile"] == "/tmp/cookies.txt"


@pytest.mark.unit
def test_search_opts_omit_cookiefile_when_not_configured() -> None:
    searcher = YtDlpAudioSearcher()
    mock_ydl = _make_mock_ydl()
    with patch("yt_dlp.YoutubeDL", return_value=mock_ydl) as mock_cls:
        searcher._search_sync("test query", 5)
    opts = mock_cls.call_args[0][0]
    assert "cookiefile" not in opts


@pytest.mark.unit
def test_search_opts_include_ignoreerrors_and_ignore_no_formats() -> None:
    searcher = YtDlpAudioSearcher()
    mock_ydl = _make_mock_ydl()
    with patch("yt_dlp.YoutubeDL", return_value=mock_ydl) as mock_cls:
        searcher._search_sync("test query", 5)
    opts = mock_cls.call_args[0][0]
    assert opts["ignoreerrors"] is True
    assert opts["ignore_no_formats_error"] is True


@pytest.mark.unit
def test_download_opts_never_include_cookiefile(tmp_path: Path) -> None:
    """Cookies are search-only — download uses the Android API fallback which
    doesn't need JS signature solving."""
    searcher = YtDlpAudioSearcher(cookie_file="/tmp/cookies.txt")
    mock_ydl = _make_mock_ydl()
    mp3 = tmp_path / "abc123.mp3"
    mp3.write_bytes(b"\x00")
    with patch("yt_dlp.YoutubeDL", return_value=mock_ydl) as mock_cls:
        searcher._download_sync("https://youtube.com/watch?v=abc", tmp_path)
    opts = mock_cls.call_args[0][0]
    assert "cookiefile" not in opts

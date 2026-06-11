"""YtDlpAudioSearcher — implements AudioSearcher via yt-dlp."""

from __future__ import annotations

import asyncio
from pathlib import Path
from typing import Any

import structlog

from altune.application.catalog.ports import AudioCandidate

_logger = structlog.get_logger(__name__)


class YtDlpAudioSearcher:
    def __init__(
        self,
        ffmpeg_location: str | None = None,
        cookie_file: str | None = None,
    ) -> None:
        self._ffmpeg_location = ffmpeg_location
        self._cookie_file = cookie_file

    async def search(self, query: str, limit: int = 5) -> list[AudioCandidate]:
        return await asyncio.to_thread(self._search_sync, query, limit)

    async def download(self, url: str, temp_dir: Path) -> Path:
        return await asyncio.to_thread(self._download_sync, url, temp_dir)

    def _search_sync(self, query: str, limit: int) -> list[AudioCandidate]:
        import yt_dlp

        opts: dict[str, Any] = {
            "quiet": True,
            "no_warnings": True,
            "default_search": "ytsearch",
            "noplaylist": True,
            "ignoreerrors": True,
            "ignore_no_formats_error": True,
        }
        if self._cookie_file:
            opts["cookiefile"] = self._cookie_file
        search_query = f"ytsearch{limit}:{query}"
        with yt_dlp.YoutubeDL(opts) as ydl:
            try:
                info = ydl.extract_info(search_query, download=False)
            except Exception:
                _logger.warning("ytdlp_search_failed", query=search_query, exc_info=True)
                return []

        if info is None:
            return []

        entries: list[dict[str, Any]] = info.get("entries", [])
        candidates: list[AudioCandidate] = []
        for entry in entries:
            if entry is None:
                continue
            title = entry.get("title", "")
            artist = entry.get("artist") or entry.get("uploader") or entry.get("channel") or ""
            duration = entry.get("duration")
            webpage_url = entry.get("webpage_url") or entry.get("url") or ""
            channel = entry.get("channel") or entry.get("uploader") or ""
            raw_cats = entry.get("categories") or []
            categories = tuple(c for c in raw_cats if isinstance(c, str))
            view_count = int(entry.get("view_count") or 0)
            follower_count = int(entry.get("channel_follower_count") or 0)
            if title and webpage_url:
                candidates.append(
                    AudioCandidate(
                        title=title,
                        artist=artist,
                        duration_seconds=int(duration) if duration else None,
                        url=webpage_url,
                        channel=channel,
                        categories=categories,
                        view_count=view_count,
                        channel_follower_count=follower_count,
                    )
                )
        return candidates

    def _download_sync(self, url: str, temp_dir: Path) -> Path:
        import yt_dlp

        output_template = str(temp_dir / "%(id)s.%(ext)s")
        opts: dict[str, Any] = {
            "format": "bestaudio/best",
            "outtmpl": output_template,
            "postprocessors": [
                {
                    "key": "FFmpegExtractAudio",
                    "preferredcodec": "mp3",
                    "preferredquality": "320",
                }
            ],
            "quiet": True,
            "no_warnings": True,
        }
        if self._ffmpeg_location:
            opts["ffmpeg_location"] = self._ffmpeg_location
        with yt_dlp.YoutubeDL(opts) as ydl:
            ydl.download([url])

        mp3_files = list(temp_dir.glob("*.mp3"))
        if not mp3_files:
            msg = f"No MP3 file produced for {url}"
            raise RuntimeError(msg)
        return mp3_files[0]

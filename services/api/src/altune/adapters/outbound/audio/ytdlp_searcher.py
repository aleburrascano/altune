"""YtDlpAudioSearcher — implements AudioSearcher via yt-dlp."""

from __future__ import annotations

import asyncio
from pathlib import Path
from typing import Any

import structlog

from altune.application.catalog.ports import AudioCandidate

_logger = structlog.get_logger(__name__)


class YtDlpAudioSearcher:
    async def search(self, query: str, limit: int = 5) -> list[AudioCandidate]:
        return await asyncio.to_thread(self._search_sync, query, limit)

    async def download(self, url: str, temp_dir: Path) -> Path:
        return await asyncio.to_thread(self._download_sync, url, temp_dir)

    def _search_sync(self, query: str, limit: int) -> list[AudioCandidate]:
        import yt_dlp

        opts: dict[str, Any] = {
            "quiet": True,
            "no_warnings": True,
            "extract_flat": True,
            "playlist_items": f"1:{limit}",
        }
        with yt_dlp.YoutubeDL(opts) as ydl:
            try:
                info = ydl.extract_info(query, download=False)
            except Exception:
                _logger.warning("ytdlp_search_failed", query=query, exc_info=True)
                return []

        if info is None:
            return []

        entries = info.get("entries", [info]) if "entries" in info else [info]
        candidates: list[AudioCandidate] = []
        for entry in entries:
            if entry is None:
                continue
            title = entry.get("title", "")
            artist = entry.get("artist") or entry.get("uploader") or entry.get("channel") or ""
            duration = entry.get("duration")
            url = entry.get("url") or entry.get("webpage_url") or entry.get("id", "")
            if title and url:
                candidates.append(AudioCandidate(
                    title=title,
                    artist=artist,
                    duration_seconds=int(duration) if duration else None,
                    url=url,
                ))
        return candidates

    def _download_sync(self, url: str, temp_dir: Path) -> Path:
        import yt_dlp

        output_template = str(temp_dir / "%(id)s.%(ext)s")
        opts: dict[str, Any] = {
            "format": "bestaudio/best",
            "outtmpl": output_template,
            "postprocessors": [{
                "key": "FFmpegExtractAudio",
                "preferredcodec": "mp3",
                "preferredquality": "320",
            }],
            "quiet": True,
            "no_warnings": True,
        }
        with yt_dlp.YoutubeDL(opts) as ydl:
            ydl.download([url])

        mp3_files = list(temp_dir.glob("*.mp3"))
        if not mp3_files:
            msg = f"No MP3 file produced for {url}"
            raise RuntimeError(msg)
        return mp3_files[0]

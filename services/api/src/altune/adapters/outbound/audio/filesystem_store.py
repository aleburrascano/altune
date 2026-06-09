"""FilesystemAudioStore — implements AudioStore for local/NFS filesystem."""

from __future__ import annotations

import shutil
from pathlib import Path

import structlog

_logger = structlog.get_logger(__name__)


class FilesystemAudioStore:
    def __init__(self, base_dir: str) -> None:
        self._base = Path(base_dir)

    async def store(self, source_path: Path, audio_ref: str) -> str:
        dest = self._base / audio_ref
        dest.parent.mkdir(parents=True, exist_ok=True)
        shutil.move(str(source_path), str(dest))
        _logger.info("audio_file_stored", audio_ref=audio_ref, size_bytes=dest.stat().st_size)
        return audio_ref

"""FakeAudioStore — in-memory test double for unit tests."""

from __future__ import annotations

from pathlib import Path


class FakeAudioStore:
    def __init__(self) -> None:
        self.stored: list[tuple[Path, str]] = []

    async def store(self, source_path: Path, audio_ref: str) -> str:
        self.stored.append((source_path, audio_ref))
        return audio_ref

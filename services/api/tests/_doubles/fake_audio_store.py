"""FakeAudioStore — in-memory test double for unit tests."""

from __future__ import annotations

from pathlib import Path


class FakeAudioStore:
    def __init__(self, existing_refs: set[str] | None = None) -> None:
        self.stored: list[tuple[Path, str]] = []
        self._existing_refs: set[str] = existing_refs or set()

    def exists(self, audio_ref: str) -> bool:
        return audio_ref in self._existing_refs

    async def store(self, source_path: Path, audio_ref: str) -> str:
        self.stored.append((source_path, audio_ref))
        self._existing_refs.add(audio_ref)
        return audio_ref

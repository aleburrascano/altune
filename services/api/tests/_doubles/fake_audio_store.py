"""FakeAudioStore — in-memory test double for unit tests."""

from __future__ import annotations

from typing import TYPE_CHECKING

if TYPE_CHECKING:
    from collections.abc import AsyncIterator
    from pathlib import Path


class FakeAudioStore:
    def __init__(
        self,
        existing_refs: set[str] | None = None,
        file_contents: dict[str, bytes] | None = None,
    ) -> None:
        self.stored: list[tuple[Path, str]] = []
        self._existing_refs: set[str] = existing_refs or set()
        self._file_contents: dict[str, bytes] = file_contents or {}

    def exists(self, audio_ref: str) -> bool:
        return audio_ref in self._existing_refs

    async def store(self, source_path: Path, audio_ref: str) -> str:
        self.stored.append((source_path, audio_ref))
        self._existing_refs.add(audio_ref)
        return audio_ref

    async def stream(self, audio_ref: str) -> AsyncIterator[bytes] | None:
        if audio_ref not in self._existing_refs:
            return None
        content = self._file_contents.get(audio_ref, b"fake-audio-bytes")

        async def _gen() -> AsyncIterator[bytes]:
            yield content

        return _gen()

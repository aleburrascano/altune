"""FakeAudioSearcher — in-memory test double for unit tests."""

from __future__ import annotations

from pathlib import Path

from altune.application.catalog.ports import AudioCandidate


class FakeAudioSearcher:
    def __init__(
        self,
        search_results: dict[str, list[AudioCandidate]] | None = None,
    ) -> None:
        self._results = search_results or {}
        self.queries: list[str] = []

    async def search(self, query: str, limit: int = 5) -> list[AudioCandidate]:
        self.queries.append(query)
        return self._results.get(query, [])

    async def download(self, url: str, temp_dir: Path) -> Path:
        out = temp_dir / "downloaded.mp3"
        out.write_bytes(b"\x00" * 100)
        return out

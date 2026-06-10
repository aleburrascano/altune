"""InMemoryArtworkCache — dict-backed ArtworkCache fake for unit tests."""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import TYPE_CHECKING

from altune.application.discovery.ports import ArtworkCacheEntry

if TYPE_CHECKING:
    from altune.domain.discovery.result_kind import ResultKind

_Key = tuple[str, str, str, str]


@dataclass
class InMemoryArtworkCache:
    entries: dict[_Key, ArtworkCacheEntry] = field(default_factory=dict)
    set_calls: list[tuple[_Key, str | None]] = field(default_factory=list)

    @staticmethod
    def _key(kind: ResultKind, title: str, subtitle: str | None, mbid: str | None) -> _Key:
        return (kind.value, title, subtitle or "", mbid or "")

    def seed(
        self,
        kind: ResultKind,
        title: str,
        subtitle: str | None,
        mbid: str | None,
        url: str | None,
    ) -> None:
        self.entries[self._key(kind, title, subtitle, mbid)] = ArtworkCacheEntry(url=url)

    async def get(
        self, kind: ResultKind, title: str, subtitle: str | None, mbid: str | None
    ) -> ArtworkCacheEntry | None:
        return self.entries.get(self._key(kind, title, subtitle, mbid))

    async def set(
        self,
        kind: ResultKind,
        title: str,
        subtitle: str | None,
        mbid: str | None,
        url: str | None,
    ) -> None:
        key = self._key(kind, title, subtitle, mbid)
        self.entries[key] = ArtworkCacheEntry(url=url)
        self.set_calls.append((key, url))

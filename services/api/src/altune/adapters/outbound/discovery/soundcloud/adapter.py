# mypy: warn_unused_ignores = False
"""SoundCloud search ACL adapter — yt-dlp strategy per ADR-0007 revision.

Slice 21. Translates yt-dlp scsearch results (entries[]) to SearchResult.
yt-dlp is sync; the adapter takes an async extractor callable so:
- Production wiring wraps yt-dlp in asyncio.to_thread (see platform/wiring.py)
- Tests inject a fake extractor that returns the captured fixture directly

Returns tracks only — yt-dlp scsearch is track-specific; SoundCloud stays
tracks-only even in discover-music-v2 (album/artist search isn't available
via scsearch).
"""

from __future__ import annotations

import asyncio
import logging
import time
from collections.abc import Awaitable, Callable
from dataclasses import dataclass
from typing import Any

from altune.application.discovery.ports import ProviderSearchResponse
from altune.domain.discovery.confidence import Confidence
from altune.domain.discovery.provider import ProviderName
from altune.domain.discovery.provider_status import ProviderStatus
from altune.domain.discovery.result_kind import ResultKind
from altune.domain.discovery.search_result import SearchResult
from altune.domain.discovery.source_ref import SourceRef

_log = logging.getLogger(__name__)


# Async extractor takes a yt-dlp query string (e.g. "scsearch5:beatles")
# and returns the raw yt-dlp info dict. Tests inject a fake; production
# wraps yt-dlp.YoutubeDL.extract_info via asyncio.to_thread.
ScExtractor = Callable[[str], Awaitable[dict[str, Any]]]


@dataclass
class SoundCloudSearchAdapter:
    """Adapter for SoundCloud via yt-dlp scsearch extraction."""

    extractor: ScExtractor

    @property
    def name(self) -> str:
        return "soundcloud"

    async def search(
        self,
        query: str,
        kinds: frozenset[ResultKind],
        limit: int,
    ) -> ProviderSearchResponse:
        if ResultKind.TRACK not in kinds:
            return ProviderSearchResponse(
                provider_name=self.name,
                status=ProviderStatus.OK,
                results=(),
                latency_ms=0,
            )
        start = time.perf_counter()
        sc_query = f"scsearch{limit}:{query}"
        try:
            info = await self.extractor(sc_query)
            latency_ms = int((time.perf_counter() - start) * 1000)
        except TimeoutError:
            raise
        except asyncio.CancelledError:
            raise
        except Exception:
            _log.exception("soundcloud scsearch failed")
            return ProviderSearchResponse(
                provider_name=self.name,
                status=ProviderStatus.ERROR,
                results=(),
                latency_ms=int((time.perf_counter() - start) * 1000),
            )
        entries = info.get("entries") or []
        results = _translate_entries(entries)
        return ProviderSearchResponse(
            provider_name=self.name,
            status=ProviderStatus.OK,
            results=results,
            latency_ms=latency_ms,
        )

    async def lookup_by_url(self, url: str) -> SearchResult | None:
        """Resolve a soundcloud.com track URL via yt-dlp's single-URL extraction."""
        try:
            info = await self.extractor(url.strip())
        except Exception:
            _log.exception("soundcloud lookup_by_url failed")
            return None
        # If yt-dlp returns a playlist-shape dict for a single URL, pick the first entry.
        if "entries" in info:
            entries = info.get("entries") or []
            for entry in entries:
                if entry is None:
                    continue
                translated = _translate_one_entry(entry)
                if translated is not None:
                    return translated
            return None
        return _translate_one_entry(info)


def _translate_entries(
    entries: list[dict[str, Any] | None],
) -> tuple[SearchResult, ...]:
    out: list[SearchResult] = []
    for entry in entries:
        if entry is None:
            # yt-dlp's ignoreerrors=True can yield None entries when a
            # specific track's extraction fails; skip them.
            continue
        translated = _translate_one_entry(entry)
        if translated is not None:
            out.append(translated)
    return tuple(out)


def _translate_one_entry(entry: dict[str, Any]) -> SearchResult | None:
    title = entry.get("title")
    uploader = entry.get("uploader") or entry.get("channel") or entry.get("uploader_id")
    sc_id = entry.get("id")
    url = entry.get("webpage_url") or entry.get("url")
    if not title or not uploader or sc_id is None or not url:
        _log.warning(
            "provider_response_malformed provider=soundcloud kind=track "
            "missing=title|uploader|id|url"
        )
        return None
    image_url = _largest_thumbnail(entry.get("thumbnails"))
    duration_raw = entry.get("duration")
    duration_seconds: int | None = (
        int(duration_raw) if isinstance(duration_raw, (int, float)) else None
    )
    extras: dict[str, object] = {
        "isrc": None,  # SC user-uploads almost never carry ISRC
        "duration_seconds": duration_seconds,
        "album": None,
        "preview_url": None,
        "uploader_id": entry.get("uploader_id"),
    }
    return SearchResult(
        kind=ResultKind.TRACK,
        title=title,
        subtitle=uploader,
        image_url=image_url,
        confidence=Confidence.LOW,
        sources=(
            SourceRef(
                provider=ProviderName.SOUNDCLOUD,
                external_id=str(sc_id),
                url=url,
            ),
        ),
        extras=extras,
    )


def _largest_thumbnail(thumbnails: list[dict[str, Any]] | None) -> str | None:
    if not thumbnails:
        return None
    # Pick the thumbnail with the largest declared width; if widths are
    # missing (e.g. the 'original' entry only carries preference), keep
    # the first url-bearing entry as a fallback.
    best_url: str | None = None
    best_width = -1
    for thumb in thumbnails:
        url = thumb.get("url")
        if not url:
            continue
        width = thumb.get("width")
        if isinstance(width, int) and width > best_width:
            best_url = str(url)
            best_width = width
        elif best_url is None:
            best_url = str(url)
    return best_url

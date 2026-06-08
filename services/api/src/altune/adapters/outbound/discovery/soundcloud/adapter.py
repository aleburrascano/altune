# mypy: warn_unused_ignores = False
"""SoundCloud search + artist content ACL adapter — yt-dlp strategy per ADR-0007.

Translates yt-dlp scsearch results (entries[]) to SearchResult. Also
implements ArtistContentProvider by extracting from user profile URLs
(soundcloud.com/<username>/tracks and /sets). yt-dlp is sync; the adapter
takes an async extractor callable so:
- Production wiring wraps yt-dlp in asyncio.to_thread (see platform/wiring.py)
- Tests inject a fake extractor that returns the captured fixture directly
"""

from __future__ import annotations

import asyncio
import logging
import re
import time
from collections.abc import Awaitable, Callable
from dataclasses import dataclass, field
from typing import Any

from altune.application.discovery.ports import ContentFetchResponse, ProviderSearchResponse
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
    """Adapter for SoundCloud via yt-dlp scsearch + artist content extraction."""

    extractor: ScExtractor
    detail_extractor: ScExtractor | None = None
    _username_cache: dict[str, str | None] = field(default_factory=dict, repr=False)
    _set_url_cache: dict[str, str] = field(default_factory=dict, repr=False)

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

    async def _resolve_username(self, artist_name: str) -> str | None:
        """Resolve an artist name to a SoundCloud username via scsearch."""
        cached = self._username_cache.get(artist_name)
        if cached is not None or artist_name in self._username_cache:
            return cached
        try:
            info = await self.extractor(f"scsearch1:{artist_name}")
            entries = info.get("entries") or []
            for entry in entries:
                if entry is None:
                    continue
                uploader_url = entry.get("uploader_url")
                if isinstance(uploader_url, str) and uploader_url:
                    from urllib.parse import urlparse

                    path = urlparse(uploader_url).path.strip("/")
                    if path:
                        self._username_cache[artist_name] = path
                        return path
        except Exception:
            _log.warning("soundcloud username resolution failed for %r", artist_name)
        self._username_cache[artist_name] = None
        return None

    async def get_artist_top_tracks(self, external_id: str, limit: int) -> ContentFetchResponse:
        """Fetch top tracks from a SoundCloud profile. external_id is the artist name."""
        start = time.perf_counter()
        username = await self._resolve_username(external_id)
        if username is None:
            return ContentFetchResponse(
                self.name, ProviderStatus.ERROR, (), int((time.perf_counter() - start) * 1000)
            )
        try:
            info = await self.extractor(f"https://soundcloud.com/{username}/tracks")
            latency_ms = int((time.perf_counter() - start) * 1000)
            entries = info.get("entries") or []
            tracks = _translate_entries(entries)
            return ContentFetchResponse(self.name, ProviderStatus.OK, tracks[:limit], latency_ms)
        except Exception:
            _log.exception("soundcloud get_artist_top_tracks failed for %r", username)
            return ContentFetchResponse(
                self.name, ProviderStatus.ERROR, (), int((time.perf_counter() - start) * 1000)
            )

    async def get_artist_albums(self, external_id: str, limit: int) -> ContentFetchResponse:
        """Fetch sets (playlists) from a SoundCloud profile as albums."""
        start = time.perf_counter()
        username = await self._resolve_username(external_id)
        if username is None:
            _log.warning("soundcloud get_artist_albums: username resolution failed for %r", external_id)
            return ContentFetchResponse(
                self.name, ProviderStatus.ERROR, (), int((time.perf_counter() - start) * 1000)
            )
        url = f"https://soundcloud.com/{username}/sets"
        for attempt in range(2):
            try:
                info = await self.extractor(url)
                entries = info.get("entries") or []
                if entries or attempt > 0:
                    break
                _log.warning("soundcloud get_artist_albums: empty entries for %r, retrying", url)
            except Exception:
                if attempt > 0:
                    _log.exception("soundcloud get_artist_albums failed for %r", url)
                    return ContentFetchResponse(
                        self.name,
                        ProviderStatus.ERROR,
                        (),
                        int((time.perf_counter() - start) * 1000),
                    )
                _log.warning("soundcloud get_artist_albums: extraction error for %r, retrying", url)
                entries = []
        latency_ms = int((time.perf_counter() - start) * 1000)
        for entry in entries:
            if entry is None:
                continue
            sc_id = entry.get("id")
            entry_url = entry.get("webpage_url") or entry.get("url")
            if sc_id is not None and entry_url:
                self._set_url_cache[str(sc_id)] = entry_url
        albums = _translate_set_entries(entries)
        _log.info("soundcloud get_artist_albums: %d sets -> %d albums for %r", len(entries), len(albums), url)
        return ContentFetchResponse(self.name, ProviderStatus.OK, albums[:limit], latency_ms)

    async def get_album_tracks(self, external_id: str, limit: int) -> ContentFetchResponse:
        """Fetch tracks from a SoundCloud set. external_id is the numeric set ID."""
        start = time.perf_counter()
        set_url = self._set_url_cache.get(external_id)
        if set_url is None:
            _log.warning("soundcloud set URL not cached for id=%s", external_id)
            return ContentFetchResponse(
                self.name, ProviderStatus.ERROR, (), int((time.perf_counter() - start) * 1000)
            )
        extractor = self.detail_extractor or self.extractor
        try:
            info = await extractor(set_url)
            latency_ms = int((time.perf_counter() - start) * 1000)
            entries = info.get("entries") or []
            tracks = _translate_entries(entries)
            return ContentFetchResponse(self.name, ProviderStatus.OK, tracks[:limit], latency_ms)
        except Exception:
            _log.exception("soundcloud get_album_tracks failed for %r", set_url)
            return ContentFetchResponse(
                self.name, ProviderStatus.ERROR, (), int((time.perf_counter() - start) * 1000)
            )


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


def _translate_set_entries(
    entries: list[dict[str, Any] | None],
) -> tuple[SearchResult, ...]:
    out: list[SearchResult] = []
    for entry in entries:
        if entry is None:
            continue
        translated = _translate_one_set_entry(entry)
        if translated is not None:
            out.append(translated)
    return tuple(out)


_SET_NOISE_RE = re.compile(r"\b(playlist|mix|best\s+of|compilation)\b", re.IGNORECASE)


def _translate_one_set_entry(entry: dict[str, Any]) -> SearchResult | None:
    """Translate a yt-dlp set/playlist entry to a SearchResult with kind=ALBUM."""
    title = entry.get("title")
    uploader = entry.get("uploader") or entry.get("channel") or entry.get("uploader_id")
    sc_id = entry.get("id")
    url = entry.get("webpage_url") or entry.get("url")
    if not title or sc_id is None or not url:
        _log.warning(
            "provider_response_malformed provider=soundcloud kind=set missing=title|id|url"
        )
        return None
    if _SET_NOISE_RE.search(title):
        return None
    image_url = _largest_thumbnail(entry.get("thumbnails"))
    playlist_count = entry.get("playlist_count")
    track_count: int | None = (
        int(playlist_count) if isinstance(playlist_count, (int, float)) else None
    )
    extras: dict[str, object] = {
        "record_type": "ep",
        "track_count": track_count,
    }
    return SearchResult(
        kind=ResultKind.ALBUM,
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

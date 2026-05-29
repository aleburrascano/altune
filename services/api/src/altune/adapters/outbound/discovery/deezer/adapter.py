# mypy: warn_unused_ignores = False
"""Deezer search ACL adapter.

Translates Deezer's /search/<kind> JSON to SearchResult tuples. Tolerant-reader:
drops malformed entries with a structured log event. discover-music-v2 adds
album + artist search alongside tracks; one `search()` call fans out to the
requested kinds concurrently. Deezer carries popularity (track `rank`, artist
`nb_fan`), normalized into `extras["popularity"]`.
"""

from __future__ import annotations

import asyncio
import logging
import math
import re
import time
from dataclasses import dataclass
from typing import TYPE_CHECKING, Any

from altune.application.discovery.ports import ProviderSearchResponse
from altune.domain.discovery.confidence import Confidence
from altune.domain.discovery.provider import ProviderName
from altune.domain.discovery.provider_status import ProviderStatus
from altune.domain.discovery.result_kind import ResultKind
from altune.domain.discovery.search_result import SearchResult
from altune.domain.discovery.source_ref import SourceRef

if TYPE_CHECKING:
    import httpx

_log = logging.getLogger(__name__)

_BASE_URL = "https://api.deezer.com"


def _log_norm(value: object, max_log10: float) -> float | None:
    """Log-normalize a popularity count to [0, 1]; None if absent/invalid."""
    if not isinstance(value, (int, float)) or value <= 0:
        return None
    return min(1.0, math.log10(float(value) + 1.0) / max_log10)


@dataclass
class DeezerSearchAdapter:
    """Adapter for Deezer's public search API. No auth required."""

    client: httpx.AsyncClient
    base_url: str = _BASE_URL

    @property
    def name(self) -> str:
        return "deezer"

    async def search(
        self,
        query: str,
        kinds: frozenset[ResultKind],
        limit: int,
    ) -> ProviderSearchResponse:
        # Fan out to each requested kind's endpoint concurrently; combine.
        plan: list[tuple[str, Any]] = []
        if ResultKind.TRACK in kinds:
            plan.append(("track", _translate_tracks))
        if ResultKind.ALBUM in kinds:
            plan.append(("album", _translate_albums))
        if ResultKind.ARTIST in kinds:
            plan.append(("artist", _translate_artists))
        if not plan:
            return ProviderSearchResponse(self.name, ProviderStatus.OK, (), 0)

        start = time.perf_counter()
        try:
            payloads = await asyncio.gather(
                *(self._fetch(endpoint, query, limit) for endpoint, _ in plan)
            )
            latency_ms = int((time.perf_counter() - start) * 1000)
        except _DeezerHTTPError as exc:
            return ProviderSearchResponse(self.name, exc.status, (), exc.latency_ms)
        except Exception:
            _log.exception("deezer search request failed")
            return ProviderSearchResponse(
                self.name, ProviderStatus.ERROR, (), int((time.perf_counter() - start) * 1000)
            )

        results: list[SearchResult] = []
        for (_, translate), payload in zip(plan, payloads, strict=True):
            results.extend(translate(payload.get("data", [])))
        return ProviderSearchResponse(self.name, ProviderStatus.OK, tuple(results), latency_ms)

    async def _fetch(self, endpoint: str, query: str, limit: int) -> dict[str, Any]:
        start = time.perf_counter()
        response = await self.client.get(
            f"{self.base_url}/search/{endpoint}", params={"q": query, "limit": limit}
        )
        latency_ms = int((time.perf_counter() - start) * 1000)
        if response.status_code == 429:
            raise _DeezerHTTPError(ProviderStatus.RATE_LIMITED, latency_ms)
        if response.status_code >= 400:
            raise _DeezerHTTPError(ProviderStatus.ERROR, latency_ms)
        try:
            payload = response.json()
        except ValueError:
            _log.warning("deezer returned non-json body")
            raise _DeezerHTTPError(ProviderStatus.ERROR, latency_ms) from None
        return payload if isinstance(payload, dict) else {}

    async def lookup_by_url(self, url: str) -> SearchResult | None:
        """Resolve a Deezer URL like https://www.deezer.com/track/<id> to a single result."""
        match = re.match(
            r"^https?://(?:www\.)?deezer\.com/(?:[a-z]{2}/)?track/(\d+)",
            url.strip(),
            re.IGNORECASE,
        )
        if match is None:
            return None
        track_id = match.group(1)
        try:
            response = await self.client.get(f"{self.base_url}/track/{track_id}")
        except Exception:
            _log.exception("deezer lookup_by_url request failed")
            return None
        if response.status_code != 200:
            return None
        try:
            entry = response.json()
        except ValueError:
            return None
        if not isinstance(entry, dict) or entry.get("error"):
            return None
        return _translate_one_track(entry)


class _DeezerHTTPError(Exception):
    """Internal: a per-endpoint HTTP failure mapped to a ProviderStatus."""

    def __init__(self, status: ProviderStatus, latency_ms: int) -> None:
        self.status = status
        self.latency_ms = latency_ms


def _translate_tracks(entries: list[dict[str, Any]]) -> tuple[SearchResult, ...]:
    return tuple(r for e in entries if (r := _translate_one_track(e)) is not None)


def _translate_albums(entries: list[dict[str, Any]]) -> tuple[SearchResult, ...]:
    return tuple(r for e in entries if (r := _translate_one_album(e)) is not None)


def _translate_artists(entries: list[dict[str, Any]]) -> tuple[SearchResult, ...]:
    return tuple(r for e in entries if (r := _translate_one_artist(e)) is not None)


def _translate_one_track(entry: dict[str, Any]) -> SearchResult | None:
    title = entry.get("title")
    artist = entry.get("artist") or {}
    artist_name = artist.get("name")
    track_id = entry.get("id")
    link = entry.get("link")
    if not title or not artist_name or track_id is None or not link:
        _log.warning(
            "provider_response_malformed provider=deezer kind=track missing=title|artist|id|link"
        )
        return None
    album = entry.get("album") or {}
    extras: dict[str, object] = {
        "isrc": entry.get("isrc"),
        "duration_seconds": entry.get("duration"),
        "album": album.get("title"),
        "preview_url": entry.get("preview") or None,
    }
    pop = _log_norm(entry.get("rank"), 6.0)  # Deezer rank is 0..~1e6.
    if pop is not None:
        extras["popularity"] = pop
    return SearchResult(
        kind=ResultKind.TRACK,
        title=title,
        subtitle=artist_name,
        image_url=album.get("cover_xl") or album.get("cover_big"),
        confidence=Confidence.LOW,
        sources=(SourceRef(provider=ProviderName.DEEZER, external_id=str(track_id), url=link),),
        extras=extras,
    )


def _translate_one_album(entry: dict[str, Any]) -> SearchResult | None:
    title = entry.get("title")
    artist = entry.get("artist") or {}
    artist_name = artist.get("name")
    album_id = entry.get("id")
    link = entry.get("link")
    if not title or not artist_name or album_id is None or not link:
        _log.warning(
            "provider_response_malformed provider=deezer kind=album missing=title|artist|id|link"
        )
        return None
    extras: dict[str, object] = {
        "isrc": None,
        "track_count": entry.get("nb_tracks"),
        "preview_url": None,
    }
    return SearchResult(
        kind=ResultKind.ALBUM,
        title=title,
        subtitle=artist_name,
        image_url=entry.get("cover_xl") or entry.get("cover_big"),
        confidence=Confidence.LOW,
        sources=(SourceRef(provider=ProviderName.DEEZER, external_id=str(album_id), url=link),),
        extras=extras,
    )


def _translate_one_artist(entry: dict[str, Any]) -> SearchResult | None:
    name = entry.get("name")
    artist_id = entry.get("id")
    link = entry.get("link")
    if not name or artist_id is None or not link:
        _log.warning("provider_response_malformed provider=deezer kind=artist missing=name|id|link")
        return None
    extras: dict[str, object] = {"isrc": None, "preview_url": None}
    pop = _log_norm(entry.get("nb_fan"), 7.0)  # fan counts reach ~1e7.
    if pop is not None:
        extras["popularity"] = pop
    return SearchResult(
        kind=ResultKind.ARTIST,
        title=name,
        subtitle=None,
        image_url=entry.get("picture_xl") or entry.get("picture_big"),
        confidence=Confidence.LOW,
        sources=(SourceRef(provider=ProviderName.DEEZER, external_id=str(artist_id), url=link),),
        extras=extras,
    )

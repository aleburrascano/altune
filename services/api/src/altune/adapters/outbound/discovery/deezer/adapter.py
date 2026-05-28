# mypy: warn_unused_ignores = False
"""Deezer search ACL adapter.

Slice 17. Translates Deezer's /search/<kind> JSON to SearchResult tuple.
Tolerant-reader: drops malformed entries with a structured log event.
v1 supports tracks only; later slices add artist/album/playlist endpoints.
"""

from __future__ import annotations

import logging
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
        if ResultKind.TRACK not in kinds:
            # v1 supports tracks only; non-track kinds fall through to other providers.
            return ProviderSearchResponse(
                provider_name=self.name,
                status=ProviderStatus.OK,
                results=(),
                latency_ms=0,
            )
        start = time.perf_counter()
        url = f"{self.base_url}/search/track"
        try:
            response = await self.client.get(url, params={"q": query, "limit": limit})
            latency_ms = int((time.perf_counter() - start) * 1000)
        except Exception:
            _log.exception("deezer search request failed")
            return ProviderSearchResponse(
                provider_name=self.name,
                status=ProviderStatus.ERROR,
                results=(),
                latency_ms=int((time.perf_counter() - start) * 1000),
            )

        if response.status_code == 429:
            return ProviderSearchResponse(
                provider_name=self.name,
                status=ProviderStatus.RATE_LIMITED,
                results=(),
                latency_ms=latency_ms,
            )
        if response.status_code >= 500 or response.status_code >= 400:
            return ProviderSearchResponse(
                provider_name=self.name,
                status=ProviderStatus.ERROR,
                results=(),
                latency_ms=latency_ms,
            )

        try:
            payload = response.json()
        except ValueError:
            _log.warning("deezer returned non-json body")
            return ProviderSearchResponse(
                provider_name=self.name,
                status=ProviderStatus.ERROR,
                results=(),
                latency_ms=latency_ms,
            )

        results = _translate_tracks(payload.get("data", []))
        return ProviderSearchResponse(
            provider_name=self.name,
            status=ProviderStatus.OK,
            results=results,
            latency_ms=latency_ms,
        )

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


def _translate_tracks(entries: list[dict[str, Any]]) -> tuple[SearchResult, ...]:
    out: list[SearchResult] = []
    for entry in entries:
        translated = _translate_one_track(entry)
        if translated is not None:
            out.append(translated)
    return tuple(out)


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
    image_url = album.get("cover_xl") or album.get("cover_big")
    extras: dict[str, object] = {
        "isrc": entry.get("isrc"),
        "duration_seconds": entry.get("duration"),
        "album": album.get("title"),
        "preview_url": None,  # reserved-null v1 per spec
    }
    return SearchResult(
        kind=ResultKind.TRACK,
        title=title,
        subtitle=artist_name,
        image_url=image_url,
        confidence=Confidence.LOW,
        sources=(
            SourceRef(
                provider=ProviderName.DEEZER,
                external_id=str(track_id),
                url=link,
            ),
        ),
        extras=extras,
    )

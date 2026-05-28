# mypy: warn_unused_ignores = False
"""Last.fm search ACL adapter.

Slice 20. Translates Last.fm's track.search response to SearchResult.
Requires an API key on the querystring (api_key param) — never logged.
Returns tracks only in v1; album.search / artist.search land later.
"""

from __future__ import annotations

import logging
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

_BASE_URL = "https://ws.audioscrobbler.com/2.0/"


@dataclass
class LastFmSearchAdapter:
    """Adapter for Last.fm's track.search API."""

    client: httpx.AsyncClient
    api_key: str
    base_url: str = _BASE_URL

    @property
    def name(self) -> str:
        return "lastfm"

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
        params = {
            "method": "track.search",
            "track": query,
            "api_key": self.api_key,
            "format": "json",
            "limit": str(limit),
        }
        try:
            response = await self.client.get(self.base_url, params=params)
            latency_ms = int((time.perf_counter() - start) * 1000)
        except Exception:
            _log.exception("lastfm search request failed")
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
        if response.status_code >= 400:
            return ProviderSearchResponse(
                provider_name=self.name,
                status=ProviderStatus.ERROR,
                results=(),
                latency_ms=latency_ms,
            )

        try:
            payload = response.json()
        except ValueError:
            _log.warning("lastfm returned non-json body")
            return ProviderSearchResponse(
                provider_name=self.name,
                status=ProviderStatus.ERROR,
                results=(),
                latency_ms=latency_ms,
            )

        results_node = payload.get("results") or {}
        track_matches = results_node.get("trackmatches") or {}
        entries = track_matches.get("track") or []
        if isinstance(entries, dict):
            # Last.fm returns a bare dict (not a list) when there's exactly one match.
            entries = [entries]
        results = _translate_tracks(entries)
        return ProviderSearchResponse(
            provider_name=self.name,
            status=ProviderStatus.OK,
            results=results,
            latency_ms=latency_ms,
        )

    async def lookup_by_url(self, url: str) -> SearchResult | None:
        # Filled in at Slice 36.
        _ = url
        return None


def _translate_tracks(entries: list[dict[str, Any]]) -> tuple[SearchResult, ...]:
    out: list[SearchResult] = []
    for entry in entries:
        translated = _translate_one_track(entry)
        if translated is not None:
            out.append(translated)
    return tuple(out)


def _translate_one_track(entry: dict[str, Any]) -> SearchResult | None:
    title = entry.get("name")
    artist_name = entry.get("artist")
    url = entry.get("url")
    if not title or not artist_name or not url:
        _log.warning(
            "provider_response_malformed provider=lastfm kind=track "
            "missing=name|artist|url"
        )
        return None
    image_url = _largest_image(entry.get("image"))
    listeners_raw = entry.get("listeners")
    try:
        listeners = int(listeners_raw) if listeners_raw is not None else None
    except (TypeError, ValueError):
        listeners = None
    extras: dict[str, object] = {
        "isrc": None,
        "duration_seconds": None,  # Last.fm track.search does not return duration
        "album": None,
        "mbid": entry.get("mbid") or None,
        "listeners": listeners,
        "preview_url": None,
    }
    return SearchResult(
        kind=ResultKind.TRACK,
        title=title,
        subtitle=artist_name,
        image_url=image_url,
        confidence=Confidence.LOW,
        sources=(
            SourceRef(
                provider=ProviderName.LASTFM,
                external_id=entry.get("mbid") or url,
                url=url,
            ),
        ),
        extras=extras,
    )


def _largest_image(images: list[dict[str, Any]] | None) -> str | None:
    if not images:
        return None
    # Last.fm returns sizes small/medium/large/extralarge; pick the largest available.
    order = {"small": 0, "medium": 1, "large": 2, "extralarge": 3, "mega": 4}
    sorted_images = sorted(
        (img for img in images if isinstance(img, dict)),
        key=lambda img: order.get(str(img.get("size", "")), -1),
        reverse=True,
    )
    for img in sorted_images:
        text = img.get("#text")
        if text:
            return str(text)
    return None

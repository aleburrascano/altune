# mypy: warn_unused_ignores = False
"""MusicBrainz search ACL adapter.

Slice 19. Translates MB's /ws/2/recording (and later /artist, /release,
/release-group) JSON to SearchResult tuple. Requires a registered
User-Agent header (set by wiring on the per-provider AsyncClient) per
https://musicbrainz.org/doc/MusicBrainz_API/Rate_Limiting.

ISRC is NOT included in default recording search; pass `inc=isrcs` to
get them. v1 omits ISRC from the wire shape for MB results — dedup
falls back to JW similarity. Future enhancement can enable `inc=isrcs`
once we've decided whether the extra payload is worth it.
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

_BASE_URL = "https://musicbrainz.org/ws/2"
_RECORDING_URL_TPL = "https://musicbrainz.org/recording/{mbid}"


@dataclass
class MusicBrainzSearchAdapter:
    """Adapter for MusicBrainz's public search API.

    User-Agent must be configured on the injected httpx client; without
    a registered UA, MB throttles to 1 req/s and may 503 (per its policy).
    """

    client: httpx.AsyncClient
    base_url: str = _BASE_URL

    @property
    def name(self) -> str:
        return "musicbrainz"

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
        url = f"{self.base_url}/recording"
        params = {"query": query, "fmt": "json", "limit": str(limit)}
        try:
            response = await self.client.get(url, params=params)
            latency_ms = int((time.perf_counter() - start) * 1000)
        except Exception:
            _log.exception("musicbrainz search request failed")
            return ProviderSearchResponse(
                provider_name=self.name,
                status=ProviderStatus.ERROR,
                results=(),
                latency_ms=int((time.perf_counter() - start) * 1000),
            )

        if response.status_code == 429 or response.status_code == 503:
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
            _log.warning("musicbrainz returned non-json body")
            return ProviderSearchResponse(
                provider_name=self.name,
                status=ProviderStatus.ERROR,
                results=(),
                latency_ms=latency_ms,
            )

        results = _translate_recordings(payload.get("recordings", []))
        return ProviderSearchResponse(
            provider_name=self.name,
            status=ProviderStatus.OK,
            results=results,
            latency_ms=latency_ms,
        )

    async def lookup_by_url(self, url: str) -> SearchResult | None:
        # Filled in at Slice 34.
        _ = url
        return None


def _translate_recordings(entries: list[dict[str, Any]]) -> tuple[SearchResult, ...]:
    out: list[SearchResult] = []
    for entry in entries:
        translated = _translate_one_recording(entry)
        if translated is not None:
            out.append(translated)
    return tuple(out)


def _translate_one_recording(entry: dict[str, Any]) -> SearchResult | None:
    title = entry.get("title")
    mbid = entry.get("id")
    credits = entry.get("artist-credit") or []
    artist_name = None
    if credits and isinstance(credits[0], dict):
        artist_name = credits[0].get("name")
    if not title or not mbid or not artist_name:
        _log.warning(
            "provider_response_malformed provider=musicbrainz kind=recording "
            "missing=title|id|artist-credit"
        )
        return None
    releases = entry.get("releases") or []
    album_title: str | None = None
    if releases and isinstance(releases[0], dict):
        album_title = releases[0].get("title")
    length_ms = entry.get("length")
    duration_seconds = int(length_ms / 1000) if isinstance(length_ms, int) else None
    extras: dict[str, object] = {
        "isrc": None,  # v1 omits; future spec can enable inc=isrcs
        "duration_seconds": duration_seconds,
        "album": album_title,
        "mbid": mbid,
        "preview_url": None,
    }
    return SearchResult(
        kind=ResultKind.TRACK,
        title=title,
        subtitle=artist_name,
        image_url=None,
        confidence=Confidence.LOW,
        sources=(
            SourceRef(
                provider=ProviderName.MUSICBRAINZ,
                external_id=mbid,
                url=_RECORDING_URL_TPL.format(mbid=mbid),
            ),
        ),
        extras=extras,
    )

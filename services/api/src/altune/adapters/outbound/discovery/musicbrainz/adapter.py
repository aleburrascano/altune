# mypy: warn_unused_ignores = False
"""MusicBrainz search ACL adapter.

Slice 19. Translates MB's /ws/2/recording, /release-group, and /artist JSON
to SearchResult tuples. Requires a registered User-Agent header (set by wiring
on the per-provider AsyncClient) per
https://musicbrainz.org/doc/MusicBrainz_API/Rate_Limiting.

discover-music-v2 adds album + artist search alongside recordings (tracks);
one `search()` call fans out to the requested kinds concurrently, each hitting
a different MB endpoint. MusicBrainz has NO popularity signal — `extras` never
carries `popularity`.

ISRC is NOT included in default recording search; we pass `inc=isrcs` so
the recording's ISRCs come back inline. The first ISRC populates
`extras["isrc"]`, reviving the canonical cross-source dedup signal with
Deezer / iTunes (ranking-overhaul addendum to ADR-0007).
"""

from __future__ import annotations

import asyncio
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

_BASE_URL = "https://musicbrainz.org/ws/2"
_RECORDING_URL_TPL = "https://musicbrainz.org/recording/{mbid}"
_RELEASE_GROUP_URL_TPL = "https://musicbrainz.org/release-group/{mbid}"
_ARTIST_URL_TPL = "https://musicbrainz.org/artist/{mbid}"


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
        # Fan out to each requested kind's endpoint concurrently; combine.
        plan: list[tuple[str, Any]] = []
        if ResultKind.TRACK in kinds:
            plan.append(("recording", _translate_recordings))
        if ResultKind.ALBUM in kinds:
            plan.append(("release-group", _translate_release_groups))
        if ResultKind.ARTIST in kinds:
            plan.append(("artist", _translate_artists))
        if not plan:
            return ProviderSearchResponse(
                provider_name=self.name,
                status=ProviderStatus.OK,
                results=(),
                latency_ms=0,
            )

        start = time.perf_counter()
        try:
            payloads = await asyncio.gather(
                *(self._fetch(endpoint, query, limit) for endpoint, _ in plan)
            )
            latency_ms = int((time.perf_counter() - start) * 1000)
        except _MusicBrainzHTTPError as exc:
            return ProviderSearchResponse(
                provider_name=self.name,
                status=exc.status,
                results=(),
                latency_ms=exc.latency_ms,
            )
        except Exception:
            _log.exception("musicbrainz search request failed")
            return ProviderSearchResponse(
                provider_name=self.name,
                status=ProviderStatus.ERROR,
                results=(),
                latency_ms=int((time.perf_counter() - start) * 1000),
            )

        results: list[SearchResult] = []
        for (endpoint, translate), payload in zip(plan, payloads, strict=True):
            results.extend(translate(payload.get(_PAYLOAD_KEY[endpoint], [])))
        return ProviderSearchResponse(
            provider_name=self.name,
            status=ProviderStatus.OK,
            results=tuple(results),
            latency_ms=latency_ms,
        )

    async def _fetch(self, endpoint: str, query: str, limit: int) -> dict[str, Any]:
        start = time.perf_counter()
        params = {"query": query, "fmt": "json", "limit": str(limit)}
        if endpoint == "recording":
            params["inc"] = "isrcs"
        response = await self.client.get(f"{self.base_url}/{endpoint}", params=params)
        latency_ms = int((time.perf_counter() - start) * 1000)
        if response.status_code == 429 or response.status_code == 503:
            raise _MusicBrainzHTTPError(ProviderStatus.RATE_LIMITED, latency_ms)
        if response.status_code >= 400:
            raise _MusicBrainzHTTPError(ProviderStatus.ERROR, latency_ms)
        try:
            payload = response.json()
        except ValueError:
            _log.warning("musicbrainz returned non-json body")
            raise _MusicBrainzHTTPError(ProviderStatus.ERROR, latency_ms) from None
        return payload if isinstance(payload, dict) else {}

    async def lookup_by_url(self, url: str) -> SearchResult | None:
        """Resolve https://musicbrainz.org/recording/<mbid> to a single result."""
        match = re.match(
            r"^https?://(?:www\.)?musicbrainz\.org/recording/([0-9a-f-]{36})",
            url.strip(),
            re.IGNORECASE,
        )
        if match is None:
            return None
        mbid = match.group(1)
        try:
            response = await self.client.get(
                f"{self.base_url}/recording/{mbid}",
                params={"fmt": "json", "inc": "artist-credits+releases"},
            )
        except Exception:
            _log.exception("musicbrainz lookup_by_url request failed")
            return None
        if response.status_code != 200:
            return None
        try:
            entry = response.json()
        except ValueError:
            return None
        if not isinstance(entry, dict):
            return None
        return _translate_one_recording(entry)


class _MusicBrainzHTTPError(Exception):
    """Internal: a per-endpoint HTTP failure mapped to a ProviderStatus."""

    def __init__(self, status: ProviderStatus, latency_ms: int) -> None:
        self.status = status
        self.latency_ms = latency_ms


# The JSON top-level array key differs per endpoint; albums use the hyphenated key.
_PAYLOAD_KEY = {
    "recording": "recordings",
    "release-group": "release-groups",
    "artist": "artists",
}


def _translate_recordings(entries: list[dict[str, Any]]) -> tuple[SearchResult, ...]:
    return tuple(r for e in entries if (r := _translate_one_recording(e)) is not None)


def _translate_release_groups(entries: list[dict[str, Any]]) -> tuple[SearchResult, ...]:
    return tuple(r for e in entries if (r := _translate_one_release_group(e)) is not None)


def _translate_artists(entries: list[dict[str, Any]]) -> tuple[SearchResult, ...]:
    return tuple(r for e in entries if (r := _translate_one_artist(e)) is not None)


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
    isrcs = entry.get("isrcs")
    isrc = isrcs[0] if isinstance(isrcs, list) and isrcs else None
    extras: dict[str, object] = {
        "isrc": isrc,  # from inc=isrcs; None when the recording has no ISRC
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


def _translate_one_release_group(entry: dict[str, Any]) -> SearchResult | None:
    title = entry.get("title")
    mbid = entry.get("id")
    credits = entry.get("artist-credit") or []
    artist_name = None
    if credits and isinstance(credits[0], dict):
        artist_name = credits[0].get("name")
    if not title or not mbid:
        _log.warning(
            "provider_response_malformed provider=musicbrainz kind=release-group missing=title|id"
        )
        return None
    first_release_date = entry.get("first-release-date")
    year = first_release_date[:4] if first_release_date else None
    extras: dict[str, object] = {
        "isrc": None,
        "preview_url": None,
        "year": year,
    }
    return SearchResult(
        kind=ResultKind.ALBUM,
        title=title,
        subtitle=artist_name,
        image_url=None,
        confidence=Confidence.LOW,
        sources=(
            SourceRef(
                provider=ProviderName.MUSICBRAINZ,
                external_id=mbid,
                url=_RELEASE_GROUP_URL_TPL.format(mbid=mbid),
            ),
        ),
        extras=extras,
    )


def _translate_one_artist(entry: dict[str, Any]) -> SearchResult | None:
    name = entry.get("name")
    mbid = entry.get("id")
    if not name or not mbid:
        _log.warning(
            "provider_response_malformed provider=musicbrainz kind=artist missing=name|id"
        )
        return None
    extras: dict[str, object] = {"isrc": None, "preview_url": None}
    return SearchResult(
        kind=ResultKind.ARTIST,
        title=name,
        subtitle=None,
        image_url=None,
        confidence=Confidence.LOW,
        sources=(
            SourceRef(
                provider=ProviderName.MUSICBRAINZ,
                external_id=mbid,
                url=_ARTIST_URL_TPL.format(mbid=mbid),
            ),
        ),
        extras=extras,
    )

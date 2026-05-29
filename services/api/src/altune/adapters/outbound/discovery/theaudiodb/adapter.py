# mypy: warn_unused_ignores = False
"""TheAudioDB search + artwork ACL adapter.

Translates TheAudioDB's JSON to SearchResult tuples. Free key "123" (30
req/min). Free-text search supports ARTIST only via search.php?s=<query>;
album/track lookups need an explicit artist name, so they are unavailable
from a free-text query (returns nothing for TRACK/ALBUM-only kinds).
Tolerant-reader: drops malformed entries with a structured log event.
TheAudioDB exposes no reliable popularity field, so `extras["popularity"]`
is never set. Also implements ArtworkResolver for best-effort cover-art
back-fill (artist thumb / album thumb / track thumb).
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

_BASE_URL = "https://www.theaudiodb.com/api/v1/json"
_DEFAULT_API_KEY = "123"


@dataclass
class TheAudioDBSearchAdapter:
    """Adapter for TheAudioDB's search API. Free key, no auth header."""

    client: httpx.AsyncClient
    api_key: str = _DEFAULT_API_KEY
    base_url: str = _BASE_URL

    @property
    def name(self) -> str:
        return "theaudiodb"

    async def search(
        self,
        query: str,
        kinds: frozenset[ResultKind],
        limit: int,
    ) -> ProviderSearchResponse:
        # Only ARTIST is reachable from a free-text query (search.php?s=).
        # TRACK/ALBUM need an explicit artist name, so they are skipped here.
        if ResultKind.ARTIST not in kinds:
            return ProviderSearchResponse(self.name, ProviderStatus.OK, (), 0)

        start = time.perf_counter()
        try:
            payload = await self._fetch("search.php", {"s": query})
            latency_ms = int((time.perf_counter() - start) * 1000)
        except _TheAudioDBHTTPError as exc:
            return ProviderSearchResponse(self.name, exc.status, (), exc.latency_ms)
        except Exception:
            _log.exception("theaudiodb search request failed")
            return ProviderSearchResponse(
                self.name, ProviderStatus.ERROR, (), int((time.perf_counter() - start) * 1000)
            )

        artists = payload.get("artists") or []
        results = _translate_artists(artists)[:limit]
        return ProviderSearchResponse(self.name, ProviderStatus.OK, results, latency_ms)

    async def _fetch(self, endpoint: str, params: dict[str, Any]) -> dict[str, Any]:
        start = time.perf_counter()
        response = await self.client.get(f"{self.base_url}/{self.api_key}/{endpoint}", params=params)
        latency_ms = int((time.perf_counter() - start) * 1000)
        if response.status_code == 429:
            raise _TheAudioDBHTTPError(ProviderStatus.RATE_LIMITED, latency_ms)
        if response.status_code >= 400:
            raise _TheAudioDBHTTPError(ProviderStatus.ERROR, latency_ms)
        try:
            payload = response.json()
        except ValueError:
            _log.warning("theaudiodb returned non-json body")
            raise _TheAudioDBHTTPError(ProviderStatus.ERROR, latency_ms) from None
        return payload if isinstance(payload, dict) else {}

    async def lookup_by_url(self, url: str) -> SearchResult | None:
        # TheAudioDB is not part of the v1 URL-paste set, so it handles no URLs.
        _ = url
        return None

    async def resolve_artwork(
        self,
        kind: ResultKind,
        title: str,
        subtitle: str | None,
    ) -> str | None:
        """Best-effort cover-art lookup for an art-less result. Never raises."""
        try:
            if kind is ResultKind.ARTIST:
                payload = await self._fetch("search.php", {"s": title})
                artists = payload.get("artists") or []
                if artists and isinstance(artists[0], dict):
                    return artists[0].get("strArtistThumb") or None
                return None
            if kind is ResultKind.ALBUM:
                payload = await self._fetch("searchalbum.php", {"s": subtitle or "", "a": title})
                albums = payload.get("album") or []
                if albums and isinstance(albums[0], dict):
                    return albums[0].get("strAlbumThumb") or None
                return None
            payload = await self._fetch("searchtrack.php", {"s": subtitle or "", "t": title})
            tracks = payload.get("track") or []
            if tracks and isinstance(tracks[0], dict):
                return tracks[0].get("strTrackThumb") or tracks[0].get("strAlbumThumb") or None
            return None
        except Exception:
            return None


class _TheAudioDBHTTPError(Exception):
    """Internal: a per-fetch HTTP failure mapped to a ProviderStatus."""

    def __init__(self, status: ProviderStatus, latency_ms: int) -> None:
        self.status = status
        self.latency_ms = latency_ms


def _translate_artists(entries: list[dict[str, Any]]) -> tuple[SearchResult, ...]:
    return tuple(r for e in entries if (r := _translate_one_artist(e)) is not None)


def _translate_one_artist(entry: dict[str, Any]) -> SearchResult | None:
    name = entry.get("strArtist")
    artist_id = entry.get("idArtist")
    if not name or artist_id is None:
        _log.warning(
            "provider_response_malformed provider=theaudiodb kind=artist "
            "missing=strArtist|idArtist"
        )
        return None
    extras: dict[str, object] = {"isrc": None, "preview_url": None}
    return SearchResult(
        kind=ResultKind.ARTIST,
        title=name,
        subtitle=None,
        image_url=entry.get("strArtistThumb") or entry.get("strArtistWideThumb"),
        confidence=Confidence.LOW,
        sources=(
            SourceRef(
                provider=ProviderName.THEAUDIODB,
                external_id=str(artist_id),
                url=f"https://www.theaudiodb.com/artist/{artist_id}",
            ),
        ),
        extras=extras,
    )

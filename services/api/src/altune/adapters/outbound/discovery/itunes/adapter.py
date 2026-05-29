# mypy: warn_unused_ignores = False
"""iTunes Search API ACL adapter.

Translates the iTunes Search API's JSON to SearchResult tuples. Free, no
auth; ~20 calls/min (covered by the per-source cache). No ISRC in the
response, but strong relevance, artwork, and preview URLs. Tolerant-reader:
drops malformed entries with a structured log event. discover-music-v2 adds
album + artist search alongside tracks; one `search()` call fans out to the
requested kinds concurrently. iTunes uses a single `/search` endpoint with a
per-kind `entity` param. No popularity field, so `extras["popularity"]` is
never set (the ranker treats absent popularity as 0).
"""

from __future__ import annotations

import asyncio
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

_BASE_URL = "https://itunes.apple.com"


@dataclass
class ITunesSearchAdapter:
    """Adapter for the iTunes Search API. No auth required."""

    client: httpx.AsyncClient
    base_url: str = _BASE_URL

    @property
    def name(self) -> str:
        return "itunes"

    async def search(
        self,
        query: str,
        kinds: frozenset[ResultKind],
        limit: int,
    ) -> ProviderSearchResponse:
        # Fan out to each requested kind's `entity` concurrently; combine.
        plan: list[tuple[str, Any]] = []
        if ResultKind.TRACK in kinds:
            plan.append(("song", _translate_tracks))
        if ResultKind.ALBUM in kinds:
            plan.append(("album", _translate_albums))
        if ResultKind.ARTIST in kinds:
            plan.append(("musicArtist", _translate_artists))
        if not plan:
            return ProviderSearchResponse(self.name, ProviderStatus.OK, (), 0)

        start = time.perf_counter()
        try:
            payloads = await asyncio.gather(
                *(self._fetch(entity, query, limit) for entity, _ in plan)
            )
            latency_ms = int((time.perf_counter() - start) * 1000)
        except _ITunesHTTPError as exc:
            return ProviderSearchResponse(self.name, exc.status, (), exc.latency_ms)
        except Exception:
            _log.exception("itunes search request failed")
            return ProviderSearchResponse(
                self.name, ProviderStatus.ERROR, (), int((time.perf_counter() - start) * 1000)
            )

        results: list[SearchResult] = []
        for (_, translate), payload in zip(plan, payloads, strict=True):
            results.extend(translate(payload.get("results", [])))
        return ProviderSearchResponse(self.name, ProviderStatus.OK, tuple(results), latency_ms)

    async def _fetch(self, entity: str, query: str, limit: int) -> dict[str, Any]:
        start = time.perf_counter()
        response = await self.client.get(
            f"{self.base_url}/search",
            params={"term": query, "media": "music", "entity": entity, "limit": limit},
        )
        latency_ms = int((time.perf_counter() - start) * 1000)
        if response.status_code == 429:
            raise _ITunesHTTPError(ProviderStatus.RATE_LIMITED, latency_ms)
        if response.status_code >= 400:
            raise _ITunesHTTPError(ProviderStatus.ERROR, latency_ms)
        try:
            payload = response.json()
        except ValueError:
            _log.warning("itunes returned non-json body")
            raise _ITunesHTTPError(ProviderStatus.ERROR, latency_ms) from None
        return payload if isinstance(payload, dict) else {}

    async def lookup_by_url(self, url: str) -> SearchResult | None:
        # iTunes / Apple Music URLs are not part of the v1 URL-paste set
        # (ADR-0007 locks paste resolution to Deezer/SoundCloud/MusicBrainz/
        # Last.fm), so this provider handles no URLs. Returning None keeps it a
        # well-behaved SearchProvider.
        _ = url
        return None


class _ITunesHTTPError(Exception):
    """Internal: a per-entity HTTP failure mapped to a ProviderStatus."""

    def __init__(self, status: ProviderStatus, latency_ms: int) -> None:
        self.status = status
        self.latency_ms = latency_ms


def _translate_tracks(entries: list[dict[str, Any]]) -> tuple[SearchResult, ...]:
    return tuple(r for e in entries if (r := _translate_one_track(e)) is not None)


def _translate_albums(entries: list[dict[str, Any]]) -> tuple[SearchResult, ...]:
    return tuple(r for e in entries if (r := _translate_one_album(e)) is not None)


def _translate_artists(entries: list[dict[str, Any]]) -> tuple[SearchResult, ...]:
    return tuple(r for e in entries if (r := _translate_one_artist(e)) is not None)


def _upscale_artwork(artwork_url: str | None) -> str | None:
    """iTunes returns a 100x100 thumbnail; request a larger render (1000px is
    the sweet spot — the CDN clamps above 3000)."""
    if not artwork_url:
        return None
    return artwork_url.replace("100x100", "1000x1000")


def _translate_one_track(entry: dict[str, Any]) -> SearchResult | None:
    title = entry.get("trackName")
    artist_name = entry.get("artistName")
    track_id = entry.get("trackId")
    view_url = entry.get("trackViewUrl")
    if not title or not artist_name or track_id is None or not view_url:
        _log.warning(
            "provider_response_malformed provider=itunes kind=track "
            "missing=trackName|artistName|trackId|trackViewUrl"
        )
        return None
    duration_ms = entry.get("trackTimeMillis")
    extras: dict[str, object] = {
        "isrc": None,  # iTunes Search API does not expose ISRC.
        "duration_seconds": int(duration_ms) // 1000 if duration_ms else None,
        "album": entry.get("collectionName"),
        "preview_url": entry.get("previewUrl"),
    }
    return SearchResult(
        kind=ResultKind.TRACK,
        title=title,
        subtitle=artist_name,
        image_url=_upscale_artwork(entry.get("artworkUrl100")),
        confidence=Confidence.LOW,
        sources=(
            SourceRef(
                provider=ProviderName.ITUNES,
                external_id=str(track_id),
                url=view_url,
            ),
        ),
        extras=extras,
    )


def _translate_one_album(entry: dict[str, Any]) -> SearchResult | None:
    title = entry.get("collectionName")
    artist_name = entry.get("artistName")
    collection_id = entry.get("collectionId")
    view_url = entry.get("collectionViewUrl")
    if not title or not artist_name or collection_id is None or not view_url:
        _log.warning(
            "provider_response_malformed provider=itunes kind=album "
            "missing=collectionName|artistName|collectionId|collectionViewUrl"
        )
        return None
    extras: dict[str, object] = {
        "isrc": None,
        "track_count": entry.get("trackCount"),
        "record_type": entry.get("collectionType"),  # Album / Single / EP / Compilation
        "preview_url": None,
    }
    return SearchResult(
        kind=ResultKind.ALBUM,
        title=title,
        subtitle=artist_name,
        image_url=_upscale_artwork(entry.get("artworkUrl100")),
        confidence=Confidence.LOW,
        sources=(
            SourceRef(
                provider=ProviderName.ITUNES,
                external_id=str(collection_id),
                url=view_url,
            ),
        ),
        extras=extras,
    )


def _translate_one_artist(entry: dict[str, Any]) -> SearchResult | None:
    name = entry.get("artistName")
    artist_id = entry.get("artistId")
    if not name or artist_id is None:
        _log.warning(
            "provider_response_malformed provider=itunes kind=artist missing=artistName|artistId"
        )
        return None
    # iTunes artist results often omit artistLinkUrl; construct the canonical
    # Apple Music artist URL as a fallback.
    view_url = entry.get("artistLinkUrl") or f"https://music.apple.com/artist/{artist_id}"
    extras: dict[str, object] = {"isrc": None, "preview_url": None}
    return SearchResult(
        kind=ResultKind.ARTIST,
        title=name,
        subtitle=None,
        image_url=None,  # iTunes artist results have no artwork.
        confidence=Confidence.LOW,
        sources=(
            SourceRef(
                provider=ProviderName.ITUNES,
                external_id=str(artist_id),
                url=view_url,
            ),
        ),
        extras=extras,
    )

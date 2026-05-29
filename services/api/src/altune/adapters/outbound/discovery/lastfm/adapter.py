# mypy: warn_unused_ignores = False
"""Last.fm search ACL adapter.

Translates Last.fm's track/album/artist .search responses to SearchResult.
Requires an API key on the querystring (api_key param) — never logged.
discover-music-v2 adds album + artist search alongside tracks; one `search()`
call fans out to the requested kinds concurrently. Last.fm carries popularity
(track/artist `listeners`), log-normalized into `extras["popularity"]`.
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
    from collections.abc import Callable

    import httpx

_log = logging.getLogger(__name__)

_BASE_URL = "https://ws.audioscrobbler.com/2.0/"

# Global Last.fm play counts reach ~10^10; log10 of that ≈ 10, so dividing by
# 10 maps playcount → roughly [0, 1].
_PLAYCOUNT_MAX_LOG10 = 10.0


def _log_norm(value: object, max_log10: float) -> float | None:
    """Log-normalize a popularity count to [0, 1]; None if absent/invalid."""
    if isinstance(value, str):
        try:
            value = int(value)
        except ValueError:
            return None
    if not isinstance(value, (int, float)) or value <= 0:
        return None
    return min(1.0, math.log10(float(value) + 1.0) / max_log10)


@dataclass
class LastFmSearchAdapter:
    """Adapter for Last.fm's track/album/artist .search API."""

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
        # Fan out to each requested kind's method concurrently; combine.
        plan: list[_KindPlan] = []
        if ResultKind.TRACK in kinds:
            plan.append(
                _KindPlan("track.search", "track", "trackmatches", "track", _translate_tracks)
            )
        if ResultKind.ALBUM in kinds:
            plan.append(
                _KindPlan("album.search", "album", "albummatches", "album", _translate_albums)
            )
        # ARTIST search is intentionally NOT queried on Last.fm: its artist DB is
        # crowd-scrobbled and riddled with mislabeled beat/track titles posing as
        # artists (e.g. "Free for Profit Che + Rest in Bass"). Cleaner artist
        # entities come from iTunes / MusicBrainz / Deezer. Last.fm stays for
        # tracks (+ its listeners popularity) and albums. (discover-music-v2)
        if not plan:
            return ProviderSearchResponse(
                provider_name=self.name,
                status=ProviderStatus.OK,
                results=(),
                latency_ms=0,
            )

        start = time.perf_counter()
        try:
            payloads = await asyncio.gather(*(self._fetch(p, query, limit) for p in plan))
            latency_ms = int((time.perf_counter() - start) * 1000)
        except _LastFmHTTPError as exc:
            return ProviderSearchResponse(
                provider_name=self.name,
                status=exc.status,
                results=(),
                latency_ms=exc.latency_ms,
            )
        except Exception:
            _log.exception("lastfm search request failed")
            return ProviderSearchResponse(
                provider_name=self.name,
                status=ProviderStatus.ERROR,
                results=(),
                latency_ms=int((time.perf_counter() - start) * 1000),
            )

        results: list[SearchResult] = []
        for p, payload in zip(plan, payloads, strict=True):
            results_node = payload.get("results") or {}
            matches = results_node.get(p.matches_key) or {}
            entries = matches.get(p.entry_key) or []
            if isinstance(entries, dict):
                # Last.fm returns a bare dict (not a list) when there's exactly one match.
                entries = [entries]
            results.extend(p.translate(entries))
        return ProviderSearchResponse(
            provider_name=self.name,
            status=ProviderStatus.OK,
            results=tuple(results),
            latency_ms=latency_ms,
        )

    async def _fetch(self, plan: _KindPlan, query: str, limit: int) -> dict[str, Any]:
        start = time.perf_counter()
        params = {
            "method": plan.method,
            plan.query_param: query,
            "api_key": self.api_key,
            "format": "json",
            "limit": str(limit),
        }
        response = await self.client.get(self.base_url, params=params)
        latency_ms = int((time.perf_counter() - start) * 1000)
        if response.status_code == 429:
            raise _LastFmHTTPError(ProviderStatus.RATE_LIMITED, latency_ms)
        if response.status_code >= 400:
            raise _LastFmHTTPError(ProviderStatus.ERROR, latency_ms)
        try:
            payload = response.json()
        except ValueError:
            _log.warning("lastfm returned non-json body")
            raise _LastFmHTTPError(ProviderStatus.ERROR, latency_ms) from None
        return payload if isinstance(payload, dict) else {}

    async def lookup_by_url(self, url: str) -> SearchResult | None:
        """Resolve https://www.last.fm/music/<Artist>/_/<Track> to a single result."""
        match = re.match(
            r"^https?://(?:www\.)?last\.fm/music/([^/]+)/_/([^/?#]+)",
            url.strip(),
            re.IGNORECASE,
        )
        if match is None:
            return None
        artist = match.group(1).replace("+", " ").replace("%20", " ")
        track = match.group(2).replace("+", " ").replace("%20", " ")
        params = {
            "method": "track.getInfo",
            "artist": artist,
            "track": track,
            "api_key": self.api_key,
            "format": "json",
        }
        try:
            response = await self.client.get(self.base_url, params=params)
        except Exception:
            _log.exception("lastfm lookup_by_url request failed")
            return None
        if response.status_code != 200:
            return None
        try:
            payload = response.json()
        except ValueError:
            return None
        track_data = payload.get("track") if isinstance(payload, dict) else None
        if not isinstance(track_data, dict):
            return None
        # Adapt track.getInfo shape to track.search shape.
        return _translate_one_track(
            {
                "name": track_data.get("name"),
                "artist": (track_data.get("artist") or {}).get("name")
                if isinstance(track_data.get("artist"), dict)
                else track_data.get("artist"),
                "url": track_data.get("url"),
                "mbid": track_data.get("mbid"),
                "image": track_data.get("album", {}).get("image")
                if isinstance(track_data.get("album"), dict)
                else None,
                "listeners": track_data.get("listeners"),
            }
        )

    async def resolve_popularity(
        self,
        kind: ResultKind,
        title: str,
        subtitle: str | None,
    ) -> float | None:
        """Uniform popularity via Last.fm getInfo play counts. Never raises.

        Keyed by (artist, title) — which we hold post-search — so it back-fills
        popularity for any result regardless of which provider surfaced it.
        """
        if kind is ResultKind.ARTIST:
            params = {"method": "artist.getInfo", "artist": title}
            path: tuple[str, ...] = ("artist", "stats", "playcount")
        elif kind is ResultKind.ALBUM:
            if not subtitle:
                return None
            params = {"method": "album.getInfo", "artist": subtitle, "album": title}
            path = ("album", "playcount")
        else:
            if not subtitle:
                return None
            params = {"method": "track.getInfo", "artist": subtitle, "track": title}
            path = ("track", "playcount")
        params |= {"api_key": self.api_key, "format": "json", "autocorrect": "1"}
        try:
            response = await self.client.get(self.base_url, params=params)
            if response.status_code != 200:
                return None
            data = response.json()
        except Exception:
            return None
        if not isinstance(data, dict) or "error" in data:
            return None
        node: Any = data
        for key in path:
            node = node.get(key) if isinstance(node, dict) else None
            if node is None:
                return None
        try:
            playcount = int(node)
        except (TypeError, ValueError):
            return None
        if playcount <= 0:
            return None
        return min(1.0, math.log10(playcount + 1.0) / _PLAYCOUNT_MAX_LOG10)


@dataclass
class _KindPlan:
    """Per-kind fetch + translate plan for one Last.fm search method."""

    method: str
    query_param: str
    matches_key: str
    entry_key: str
    translate: Callable[[list[dict[str, Any]]], tuple[SearchResult, ...]]


class _LastFmHTTPError(Exception):
    """Internal: a per-method HTTP failure mapped to a ProviderStatus."""

    def __init__(self, status: ProviderStatus, latency_ms: int) -> None:
        self.status = status
        self.latency_ms = latency_ms


def _translate_tracks(entries: list[dict[str, Any]]) -> tuple[SearchResult, ...]:
    return tuple(r for e in entries if (r := _translate_one_track(e)) is not None)


def _translate_albums(entries: list[dict[str, Any]]) -> tuple[SearchResult, ...]:
    return tuple(r for e in entries if (r := _translate_one_album(e)) is not None)


def _translate_one_track(entry: dict[str, Any]) -> SearchResult | None:
    title = entry.get("name")
    artist_name = entry.get("artist")
    url = entry.get("url")
    if not title or not artist_name or not url:
        _log.warning(
            "provider_response_malformed provider=lastfm kind=track missing=name|artist|url"
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
    pop = _log_norm(listeners, 7.0)  # listener counts reach ~1e7.
    if pop is not None:
        extras["popularity"] = pop
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


def _translate_one_album(entry: dict[str, Any]) -> SearchResult | None:
    title = entry.get("name")
    artist_name = entry.get("artist")
    url = entry.get("url")
    if not title or not url:
        _log.warning("provider_response_malformed provider=lastfm kind=album missing=name|url")
        return None
    extras: dict[str, object] = {"isrc": None, "preview_url": None}
    return SearchResult(
        kind=ResultKind.ALBUM,
        title=title,
        subtitle=artist_name or None,
        image_url=_largest_image(entry.get("image")),
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

# mypy: warn_unused_ignores = False
"""RedisArtworkCache — final artwork-waterfall outcome cache.

One key per (kind, title, subtitle, mbid). A hit skips the whole
Fanart.tv → Genius → Deezer/TheAudioDB chain on repeat searches.
Positive outcomes live 14 days; negatives 24h (new art may appear).
Redis errors degrade to a miss; never raises.
"""

from __future__ import annotations

import hashlib
import logging
from typing import TYPE_CHECKING

from altune.application.discovery.ports import ArtworkCacheEntry

if TYPE_CHECKING:
    from redis.asyncio import Redis

    from altune.domain.discovery.result_kind import ResultKind

_log = logging.getLogger(__name__)
_KEY_PREFIX = "discovery:artwork:v1"
_POSITIVE_TTL_SECONDS = 14 * 86400
_NEGATIVE_TTL_SECONDS = 24 * 3600
_NEGATIVE_SENTINEL = "__none__"


def artwork_cache_key(kind: ResultKind, title: str, subtitle: str | None, mbid: str | None) -> str:
    raw = f"{title}|{subtitle or ''}|{mbid or ''}"
    digest = hashlib.sha256(raw.encode("utf-8")).hexdigest()[:32]
    return f"{_KEY_PREFIX}:{kind.value}:{digest}"


class RedisArtworkCache:
    """ArtworkCache implementation on Redis."""

    def __init__(self, redis: Redis) -> None:  # type: ignore[type-arg]
        self._redis = redis

    async def get(
        self,
        kind: ResultKind,
        title: str,
        subtitle: str | None,
        mbid: str | None,
    ) -> ArtworkCacheEntry | None:
        try:
            val = await self._redis.get(artwork_cache_key(kind, title, subtitle, mbid))
        except Exception:
            _log.warning("artwork_cache_unavailable op=get", exc_info=True)
            return None
        if val is None:
            return None
        decoded = val.decode() if isinstance(val, bytes) else str(val)
        return ArtworkCacheEntry(url=None if decoded == _NEGATIVE_SENTINEL else decoded)

    async def set(
        self,
        kind: ResultKind,
        title: str,
        subtitle: str | None,
        mbid: str | None,
        url: str | None,
    ) -> None:
        value = url if url is not None else _NEGATIVE_SENTINEL
        ttl = _POSITIVE_TTL_SECONDS if url is not None else _NEGATIVE_TTL_SECONDS
        try:
            await self._redis.setex(artwork_cache_key(kind, title, subtitle, mbid), ttl, value)
        except Exception:
            _log.warning("artwork_cache_unavailable op=set", exc_info=True)

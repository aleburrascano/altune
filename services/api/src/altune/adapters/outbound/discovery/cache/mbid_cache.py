# mypy: warn_unused_ignores = False
"""CachedMbidResolver — Redis cache around the MB URL-lookup resolver.

Provider-URL → MBID mappings are stable, so positive entries live 30 days.
Misses are cached too (24h) so a search doesn't re-probe MB's /url endpoint
for artists MB has no link for. Redis errors degrade to the inner resolver.
"""

from __future__ import annotations

import hashlib
import logging
from typing import TYPE_CHECKING

if TYPE_CHECKING:
    from redis.asyncio import Redis

    from altune.application.discovery.ports import MbidResolver

_log = logging.getLogger(__name__)
_KEY_PREFIX = "discovery:mbid:v1"
_POSITIVE_TTL_SECONDS = 30 * 86400  # 30 days — ID mappings rarely change
_NEGATIVE_TTL_SECONDS = 24 * 3600  # 24 hours — MB may gain the link later
_NEGATIVE_SENTINEL = "__none__"


def mbid_cache_key(provider_url: str) -> str:
    digest = hashlib.sha256(provider_url.encode("utf-8")).hexdigest()[:32]
    return f"{_KEY_PREFIX}:{digest}"


class CachedMbidResolver:
    """MbidResolver decorator: Redis cache in front of the live MB lookup."""

    def __init__(self, inner: MbidResolver, redis: Redis) -> None:  # type: ignore[type-arg]
        self._inner = inner
        self._redis = redis

    async def resolve(self, provider_url: str) -> str | None:
        key = mbid_cache_key(provider_url)
        try:
            cached = await self._redis.get(key)
        except Exception:
            _log.warning("mbid_cache_unavailable op=get", exc_info=True)
            cached = None
        if cached is not None:
            decoded = cached.decode() if isinstance(cached, bytes) else str(cached)
            return None if decoded == _NEGATIVE_SENTINEL else decoded

        mbid = await self._inner.resolve(provider_url)

        value = mbid if mbid is not None else _NEGATIVE_SENTINEL
        ttl = _POSITIVE_TTL_SECONDS if mbid is not None else _NEGATIVE_TTL_SECONDS
        try:
            await self._redis.setex(key, ttl, value)
        except Exception:
            _log.warning("mbid_cache_unavailable op=set", exc_info=True)
        return mbid

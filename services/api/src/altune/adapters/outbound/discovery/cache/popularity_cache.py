# mypy: warn_unused_ignores = False
"""CachedPopularityResolver — Redis cache around the uniform popularity lookup.

Positive values live 7 days (playcounts drift slowly). Misses are cached 2h —
short, because the inner resolver also returns None on transient Last.fm
errors and a pinned false-negative would zero a genuine track's popularity.
Redis errors degrade to the inner resolver.
"""

from __future__ import annotations

import hashlib
import logging
from typing import TYPE_CHECKING

if TYPE_CHECKING:
    from redis.asyncio import Redis

    from altune.application.discovery.ports import PopularityResolver
    from altune.domain.discovery.result_kind import ResultKind

_log = logging.getLogger(__name__)
_KEY_PREFIX = "discovery:popularity:v1"
_POSITIVE_TTL_SECONDS = 7 * 86400
_NEGATIVE_TTL_SECONDS = 2 * 3600
_NEGATIVE_SENTINEL = "__none__"


def popularity_cache_key(kind: ResultKind, title: str, subtitle: str | None) -> str:
    raw = f"{title}|{subtitle or ''}"
    digest = hashlib.sha256(raw.encode("utf-8")).hexdigest()[:32]
    return f"{_KEY_PREFIX}:{kind.value}:{digest}"


class CachedPopularityResolver:
    """PopularityResolver decorator: Redis cache in front of Last.fm getInfo."""

    def __init__(self, inner: PopularityResolver, redis: Redis) -> None:  # type: ignore[type-arg]
        self._inner = inner
        self._redis = redis

    async def resolve_popularity(
        self,
        kind: ResultKind,
        title: str,
        subtitle: str | None,
    ) -> float | None:
        key = popularity_cache_key(kind, title, subtitle)
        try:
            cached = await self._redis.get(key)
        except Exception:
            _log.warning("popularity_cache_unavailable op=get", exc_info=True)
            cached = None
        if cached is not None:
            decoded = cached.decode() if isinstance(cached, bytes) else str(cached)
            if decoded == _NEGATIVE_SENTINEL:
                return None
            try:
                return float(decoded)
            except ValueError:
                pass  # corrupt entry — fall through to the live lookup

        value = await self._inner.resolve_popularity(kind, title, subtitle)

        stored = str(value) if value is not None else _NEGATIVE_SENTINEL
        ttl = _POSITIVE_TTL_SECONDS if value is not None else _NEGATIVE_TTL_SECONDS
        try:
            await self._redis.setex(key, ttl, stored)
        except Exception:
            _log.warning("popularity_cache_unavailable op=set", exc_info=True)
        return value

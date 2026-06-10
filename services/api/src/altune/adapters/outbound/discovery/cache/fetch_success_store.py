# mypy: warn_unused_ignores = False
"""RedisFetchSuccessStore — sliding-window fetch success rate.

Stores per-(provider, external_id) fetch outcomes as a bounded list in Redis.
The success rate is computed from the last N entries (sliding window).
Feeds the fetch_success signal in the quality scorer.
"""

from __future__ import annotations

import logging
from typing import TYPE_CHECKING

if TYPE_CHECKING:
    from redis.asyncio import Redis

_log = logging.getLogger(__name__)
_WINDOW_SIZE = 10
_TTL_SECONDS = 172800  # 48 hours
_KEY_PREFIX = "discovery:fetch_success"


def _cache_key(provider: str, external_id: str) -> str:
    return f"{_KEY_PREFIX}:{provider}:{external_id}"


class RedisFetchSuccessStore:
    """Redis-backed sliding-window fetch success store (AC#8)."""

    def __init__(self, redis: Redis) -> None:  # type: ignore[type-arg]
        self._redis = redis

    async def get_rate(self, provider: str, external_id: str) -> float:
        key = _cache_key(provider, external_id)
        try:
            entries = await self._redis.lrange(key, 0, _WINDOW_SIZE - 1)
        except Exception:
            _log.warning("fetch_success_store_unavailable op=get_rate", exc_info=True)
            return 1.0
        if not entries:
            return 1.0
        successes = sum(1 for e in entries if (e.decode() if isinstance(e, bytes) else e) == "1")
        return successes / len(entries)

    async def record(self, provider: str, external_id: str, *, success: bool) -> None:
        key = _cache_key(provider, external_id)
        val = "1" if success else "0"
        try:
            pipe = self._redis.pipeline()
            pipe.lpush(key, val)
            pipe.ltrim(key, 0, _WINDOW_SIZE - 1)
            pipe.expire(key, _TTL_SECONDS)
            await pipe.execute()
        except Exception:
            _log.warning("fetch_success_store_unavailable op=record", exc_info=True)

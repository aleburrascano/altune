# mypy: warn_unused_ignores = False
"""RedisContentValidationCache — content fetch outcome cache.

Stores per-(provider, external_id) fetch outcomes with a 2-hour TTL.
Used by the quality gate to filter results with all sources unfetchable.
"""

from __future__ import annotations

import logging
from typing import TYPE_CHECKING

from altune.domain.discovery.content_validation_status import ContentValidationStatus

if TYPE_CHECKING:
    from redis.asyncio import Redis

_log = logging.getLogger(__name__)
_TTL_SECONDS = 7200  # 2 hours
_KEY_PREFIX = "discovery:validation"


def _cache_key(provider: str, external_id: str) -> str:
    return f"{_KEY_PREFIX}:{provider}:{external_id}"


class RedisContentValidationCache:
    """Redis-backed content validation cache (AC#14)."""

    def __init__(self, redis: Redis) -> None:  # type: ignore[type-arg]
        self._redis = redis

    async def get(self, provider: str, external_id: str) -> ContentValidationStatus:
        try:
            val = await self._redis.get(_cache_key(provider, external_id))
        except Exception:
            _log.warning("content_validation_cache_unavailable op=get", exc_info=True)
            return ContentValidationStatus.UNKNOWN
        if val is None:
            return ContentValidationStatus.UNKNOWN
        decoded = val.decode() if isinstance(val, bytes) else str(val)
        try:
            return ContentValidationStatus(decoded)
        except ValueError:
            return ContentValidationStatus.UNKNOWN

    async def record(
        self, provider: str, external_id: str, status: ContentValidationStatus
    ) -> None:
        try:
            await self._redis.setex(_cache_key(provider, external_id), _TTL_SECONDS, status.value)
        except Exception:
            _log.warning("content_validation_cache_unavailable op=record", exc_info=True)

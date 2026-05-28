# mypy: warn_unused_ignores = False
"""RedisQueryCache — slice 28 cache adapter.

Per ADR-0007 §3.4: stores post-ACL SearchResult tuples keyed by
`discovery:v1:<source>:<kinds_sorted_csv>:<sha256_of_query_norm>`. v1
prefix is the version invalidator. Per-source TTL.

Cache misses + Redis errors both return None — the use case falls
through to a live provider call. Redis outage degrades gracefully.
"""

from __future__ import annotations

import hashlib
import json
import logging
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import TYPE_CHECKING, Any

from altune.domain.discovery.confidence import Confidence
from altune.domain.discovery.provider import ProviderName
from altune.domain.discovery.result_kind import ResultKind
from altune.domain.discovery.search_result import SearchResult
from altune.domain.discovery.source_ref import SourceRef

if TYPE_CHECKING:
    from datetime import timedelta

    from redis.asyncio import Redis

_log = logging.getLogger(__name__)

_VERSION_PREFIX = "v1"
_ROOT_NAMESPACE = "discovery"


def cache_key(version: str, provider: str, query_norm: str, kinds: frozenset[ResultKind]) -> str:
    """Build the canonical cache key per ADR-0007."""
    kinds_csv = ",".join(sorted(k.value for k in kinds))
    q_hash = hashlib.sha256(query_norm.encode("utf-8")).hexdigest()[:32]
    return f"{_ROOT_NAMESPACE}:{version}:{provider}:{kinds_csv}:{q_hash}"


@dataclass
class RedisQueryCache:
    """Async Redis-backed cache for per-source SearchResult tuples."""

    redis: Redis
    version: str = _VERSION_PREFIX

    async def get(
        self,
        provider: str,
        query_norm: str,
        kinds: frozenset[ResultKind],
    ) -> tuple[tuple[SearchResult, ...], datetime] | None:
        key = cache_key(self.version, provider, query_norm, kinds)
        try:
            raw = await self.redis.get(key)
        except Exception:
            _log.warning("cache_unavailable op=get key=%s", key, exc_info=True)
            return None
        if raw is None:
            return None
        try:
            payload = json.loads(raw)
            results = tuple(_deserialize_result(item) for item in payload["results"])
            fetched_at = datetime.fromisoformat(payload["fetched_at"])
            return results, fetched_at
        except (KeyError, ValueError, TypeError):
            _log.warning("cache_decode_failed key=%s", key, exc_info=True)
            return None

    async def set(
        self,
        provider: str,
        query_norm: str,
        kinds: frozenset[ResultKind],
        results: tuple[SearchResult, ...],
        ttl: timedelta,
    ) -> None:
        key = cache_key(self.version, provider, query_norm, kinds)
        payload = json.dumps(
            {
                "results": [_serialize_result(r) for r in results],
                "fetched_at": datetime.now(UTC).isoformat(),
            }
        )
        ttl_seconds = max(1, int(ttl.total_seconds()))
        try:
            await self.redis.setex(key, ttl_seconds, payload)
        except Exception:
            _log.warning("cache_unavailable op=set key=%s", key, exc_info=True)


def _serialize_result(r: SearchResult) -> dict[str, Any]:
    return {
        "kind": r.kind.value,
        "title": r.title,
        "subtitle": r.subtitle,
        "image_url": r.image_url,
        "confidence": r.confidence.value,
        "sources": [
            {
                "provider": s.provider.value,
                "external_id": s.external_id,
                "url": s.url,
            }
            for s in r.sources
        ],
        "extras": dict(r.extras),
    }


def _deserialize_result(d: dict[str, Any]) -> SearchResult:
    return SearchResult(
        kind=ResultKind(d["kind"]),
        title=d["title"],
        subtitle=d["subtitle"],
        image_url=d["image_url"],
        confidence=Confidence(d["confidence"]),
        sources=tuple(
            SourceRef(
                provider=ProviderName(s["provider"]),
                external_id=s["external_id"],
                url=s["url"],
            )
            for s in d["sources"]
        ),
        extras=dict(d["extras"]),
    )

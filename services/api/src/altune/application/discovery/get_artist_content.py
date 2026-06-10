"""GetArtistContent — use cases for artist top tracks and albums.

AC#17: GET /v1/discovery/artists/{provider}/{id}/top-tracks
AC#18: GET /v1/discovery/artists/{provider}/{id}/albums
"""

from __future__ import annotations

from dataclasses import dataclass
from typing import TYPE_CHECKING

from altune.domain.discovery.content_validation_status import ContentValidationStatus
from altune.domain.discovery.provider_status import ProviderStatus
from altune.domain.discovery.search_result import SearchResult

if TYPE_CHECKING:
    from altune.application.discovery.ports import (
        ArtistContentProvider,
        ContentFetchResponse,
        ContentValidationCache,
        FetchSuccessStore,
    )


@dataclass(frozen=True, slots=True)
class GetArtistTopTracksInput:
    provider: str
    external_id: str
    limit: int = 5


@dataclass(frozen=True, slots=True)
class GetArtistAlbumsInput:
    provider: str
    external_id: str
    limit: int = 10


async def _record_outcome(
    cache: ContentValidationCache | None,
    store: FetchSuccessStore | None,
    provider: str,
    external_id: str,
    *,
    success: bool,
) -> None:
    status = ContentValidationStatus.FETCHABLE if success else ContentValidationStatus.UNFETCHABLE
    if cache is not None:
        await cache.record(provider, external_id, status)
    if store is not None:
        await store.record(provider, external_id, success=success)


@dataclass
class GetArtistTopTracks:
    """Fetch artist's top tracks from a single provider."""

    providers: dict[str, ArtistContentProvider]
    content_validation_cache: ContentValidationCache | None = None
    fetch_success_store: FetchSuccessStore | None = None

    async def execute(self, request: GetArtistTopTracksInput) -> ContentFetchResponse:
        provider = self.providers.get(request.provider)
        if provider is None:
            from altune.application.discovery.ports import ContentFetchResponse

            await _record_outcome(
                self.content_validation_cache,
                self.fetch_success_store,
                request.provider,
                request.external_id,
                success=False,
            )
            return ContentFetchResponse(
                provider_name=request.provider,
                status=ProviderStatus.ERROR,
                items=(),
                latency_ms=0,
            )

        response = await provider.get_artist_top_tracks(request.external_id, request.limit)
        fetchable = response.status is ProviderStatus.OK and len(response.items) > 0
        await _record_outcome(
            self.content_validation_cache,
            self.fetch_success_store,
            request.provider,
            request.external_id,
            success=fetchable,
        )
        return response


def _dedup_albums(items: tuple[SearchResult, ...]) -> tuple[SearchResult, ...]:
    """Deduplicate albums by normalized title, keeping the version with the most tracks."""
    from altune.application.discovery.normalize import normalize_for_match

    groups: dict[str, SearchResult] = {}
    for item in items:
        key = normalize_for_match(item.title)
        existing = groups.get(key)
        if existing is None:
            groups[key] = item
        else:
            existing_count = existing.extras.get("track_count", 0)
            new_count = item.extras.get("track_count", 0)
            if (
                isinstance(new_count, int)
                and isinstance(existing_count, int)
                and new_count > existing_count
            ):
                winner, loser = item, existing
            else:
                winner, loser = existing, item
            seen: set[tuple[object, str]] = set()
            merged_sources = []
            for s in (*winner.sources, *loser.sources):
                k = (s.provider, s.external_id)
                if k not in seen:
                    seen.add(k)
                    merged_sources.append(s)
            groups[key] = SearchResult(
                kind=winner.kind,
                title=winner.title,
                subtitle=winner.subtitle,
                image_url=winner.image_url or loser.image_url,
                confidence=winner.confidence,
                sources=tuple(merged_sources),
                extras=winner.extras,
            )
    return tuple(groups.values())


@dataclass
class GetArtistAlbums:
    """Fetch artist's albums from a single provider, deduped by title."""

    providers: dict[str, ArtistContentProvider]
    content_validation_cache: ContentValidationCache | None = None
    fetch_success_store: FetchSuccessStore | None = None

    async def execute(self, request: GetArtistAlbumsInput) -> ContentFetchResponse:
        provider = self.providers.get(request.provider)
        if provider is None:
            from altune.application.discovery.ports import ContentFetchResponse

            await _record_outcome(
                self.content_validation_cache,
                self.fetch_success_store,
                request.provider,
                request.external_id,
                success=False,
            )
            return ContentFetchResponse(
                provider_name=request.provider,
                status=ProviderStatus.ERROR,
                items=(),
                latency_ms=0,
            )

        response = await provider.get_artist_albums(request.external_id, request.limit)
        fetchable = response.status is ProviderStatus.OK and len(response.items) > 0
        await _record_outcome(
            self.content_validation_cache,
            self.fetch_success_store,
            request.provider,
            request.external_id,
            success=fetchable,
        )
        from altune.application.discovery.ports import ContentFetchResponse as CFR

        return CFR(
            provider_name=response.provider_name,
            status=response.status,
            items=_dedup_albums(response.items),
            latency_ms=response.latency_ms,
        )

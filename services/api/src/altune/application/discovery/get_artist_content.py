"""GetArtistContent — use cases for artist top tracks and albums.

AC#17: GET /v1/discovery/artists/{provider}/{id}/top-tracks
AC#18: GET /v1/discovery/artists/{provider}/{id}/albums
"""

from __future__ import annotations

from dataclasses import dataclass
from typing import TYPE_CHECKING

if TYPE_CHECKING:
    from altune.application.discovery.ports import ArtistContentProvider, ContentFetchResponse
    from altune.domain.discovery.search_result import SearchResult


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


@dataclass
class GetArtistTopTracks:
    """Fetch artist's top tracks from a single provider."""

    providers: dict[str, ArtistContentProvider]

    async def execute(self, request: GetArtistTopTracksInput) -> ContentFetchResponse:
        provider = self.providers.get(request.provider)
        if provider is None:
            from altune.application.discovery.ports import ContentFetchResponse
            from altune.domain.discovery.provider_status import ProviderStatus

            return ContentFetchResponse(
                provider_name=request.provider,
                status=ProviderStatus.ERROR,
                items=(),
                latency_ms=0,
            )

        return await provider.get_artist_top_tracks(request.external_id, request.limit)


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
            if isinstance(new_count, int) and isinstance(existing_count, int) and new_count > existing_count:
                groups[key] = item
    return tuple(groups.values())


@dataclass
class GetArtistAlbums:
    """Fetch artist's albums from a single provider, deduped by title."""

    providers: dict[str, ArtistContentProvider]

    async def execute(self, request: GetArtistAlbumsInput) -> ContentFetchResponse:
        provider = self.providers.get(request.provider)
        if provider is None:
            from altune.application.discovery.ports import ContentFetchResponse
            from altune.domain.discovery.provider_status import ProviderStatus

            return ContentFetchResponse(
                provider_name=request.provider,
                status=ProviderStatus.ERROR,
                items=(),
                latency_ms=0,
            )

        response = await provider.get_artist_albums(request.external_id, request.limit)
        from altune.application.discovery.ports import ContentFetchResponse as CFR

        return CFR(
            provider_name=response.provider_name,
            status=response.status,
            items=_dedup_albums(response.items),
            latency_ms=response.latency_ms,
        )

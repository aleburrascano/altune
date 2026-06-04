"""GetArtistContent — use cases for artist top tracks and albums.

AC#17: GET /v1/discovery/artists/{provider}/{id}/top-tracks
AC#18: GET /v1/discovery/artists/{provider}/{id}/albums
"""

from __future__ import annotations

from dataclasses import dataclass
from typing import TYPE_CHECKING

if TYPE_CHECKING:
    from altune.application.discovery.ports import ArtistContentProvider, ContentFetchResponse


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


@dataclass
class GetArtistAlbums:
    """Fetch artist's albums from a single provider."""

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

        return await provider.get_artist_albums(request.external_id, request.limit)

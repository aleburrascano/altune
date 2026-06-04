"""GetAlbumTracks — use case for GET /v1/discovery/albums/{provider}/{id}/tracks.

AC#14: Fetch tracks from an album by provider + external ID.
"""

from __future__ import annotations

from dataclasses import dataclass
from typing import TYPE_CHECKING

if TYPE_CHECKING:
    from altune.application.discovery.ports import AlbumContentProvider, ContentFetchResponse


@dataclass(frozen=True, slots=True)
class GetAlbumTracksInput:
    provider: str
    external_id: str
    limit: int = 50


@dataclass
class GetAlbumTracks:
    """Fetch album tracks from a single provider.

    Unlike search (scatter-gather over all providers), content fetch targets
    a specific provider + ID because the client already has a SourceRef.
    """

    providers: dict[str, AlbumContentProvider]

    async def execute(self, request: GetAlbumTracksInput) -> ContentFetchResponse:
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

        return await provider.get_album_tracks(request.external_id, request.limit)

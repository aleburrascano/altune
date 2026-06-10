"""GetAlbumTracks — use case for GET /v1/discovery/albums/{provider}/{id}/tracks.

AC#14: Fetch tracks from an album by provider + external ID.
Records content validation status for quality gate self-healing (AC#12-14).
"""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import TYPE_CHECKING

from altune.domain.discovery.content_validation_status import ContentValidationStatus
from altune.domain.discovery.provider_status import ProviderStatus

if TYPE_CHECKING:
    from altune.application.discovery.ports import (
        AlbumContentProvider,
        ContentFetchResponse,
        ContentValidationCache,
        FetchSuccessStore,
    )


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
    Records fetch outcome in validation cache + success store for quality gates.
    """

    providers: dict[str, AlbumContentProvider]
    content_validation_cache: ContentValidationCache | None = None
    fetch_success_store: FetchSuccessStore | None = None

    async def _record_outcome(self, provider: str, external_id: str, *, success: bool) -> None:
        status = (
            ContentValidationStatus.FETCHABLE if success else ContentValidationStatus.UNFETCHABLE
        )
        if self.content_validation_cache is not None:
            await self.content_validation_cache.record(provider, external_id, status)
        if self.fetch_success_store is not None:
            await self.fetch_success_store.record(provider, external_id, success=success)

    async def execute(self, request: GetAlbumTracksInput) -> ContentFetchResponse:
        provider = self.providers.get(request.provider)
        if provider is None:
            from altune.application.discovery.ports import ContentFetchResponse

            await self._record_outcome(request.provider, request.external_id, success=False)
            return ContentFetchResponse(
                provider_name=request.provider,
                status=ProviderStatus.ERROR,
                items=(),
                latency_ms=0,
            )

        response = await provider.get_album_tracks(request.external_id, request.limit)
        fetchable = response.status is ProviderStatus.OK and len(response.items) > 0
        await self._record_outcome(request.provider, request.external_id, success=fetchable)
        return response

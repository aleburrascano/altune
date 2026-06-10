"""FakeArtistTrackTitleSource — canned track titles for Genius hint tests.

Stub + call recording: returns a ContentFetchResponse built from ``titles``
and records (external_id, limit) per call so tests can assert gating.
"""

from __future__ import annotations

from dataclasses import dataclass, field

from altune.application.discovery.ports import ContentFetchResponse
from altune.domain.discovery.confidence import Confidence
from altune.domain.discovery.provider import ProviderName
from altune.domain.discovery.provider_status import ProviderStatus
from altune.domain.discovery.result_kind import ResultKind
from altune.domain.discovery.search_result import SearchResult
from altune.domain.discovery.source_ref import SourceRef


@dataclass
class FakeArtistTrackTitleSource:
    titles: tuple[str, ...] = ()
    raises: bool = False
    calls: list[tuple[str, int]] = field(default_factory=list)

    async def get_artist_top_tracks(self, external_id: str, limit: int) -> ContentFetchResponse:
        self.calls.append((external_id, limit))
        if self.raises:
            raise RuntimeError("title source down")
        items = tuple(
            SearchResult(
                kind=ResultKind.TRACK,
                title=title,
                subtitle="Artist",
                image_url=None,
                confidence=Confidence.LOW,
                sources=(
                    SourceRef(
                        provider=ProviderName.MUSICBRAINZ,
                        external_id=f"rec-{i}",
                        url=f"https://musicbrainz.org/recording/rec-{i}",
                    ),
                ),
                extras={},
            )
            for i, title in enumerate(self.titles)
        )
        return ContentFetchResponse(
            provider_name="musicbrainz",
            status=ProviderStatus.OK,
            items=items,
            latency_ms=0,
        )

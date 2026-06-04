"""Discovery application ports.

Per ADR-0007 hexagonal layout: SearchProvider, QueryCache,
SearchHistoryRepository, SearchClickRepository. Adapters in
adapters/outbound/discovery/ implement these.
"""

from __future__ import annotations

from dataclasses import dataclass
from datetime import timedelta  # noqa: TC003
from typing import TYPE_CHECKING, Protocol, runtime_checkable

if TYPE_CHECKING:
    from datetime import datetime
    from uuid import UUID

    from altune.domain.discovery.provider_status import ProviderStatus
    from altune.domain.discovery.result_kind import ResultKind
    from altune.domain.discovery.search_click import SearchClick
    from altune.domain.discovery.search_history_entry import SearchHistoryEntry
    from altune.domain.discovery.search_result import SearchResult
    from altune.domain.shared.user_id import UserId


@dataclass(frozen=True, slots=True)
class ProviderSearchResponse:
    """One provider's slice of a scatter-gather response.

    Status indicates the outcome; results is empty for non-OK statuses.
    """

    provider_name: str  # "deezer" | "musicbrainz" | "soundcloud" | "lastfm"
    status: ProviderStatus
    results: tuple[SearchResult, ...]
    latency_ms: int


@runtime_checkable
class SearchProvider(Protocol):
    """One external music source the use case fans out to.

    Adapter implementations live in adapters/outbound/discovery/<source>/.
    Translation from provider DTOs to SearchResult happens here, not in
    the use case.
    """

    @property
    def name(self) -> str: ...

    async def search(
        self,
        query: str,
        kinds: frozenset[ResultKind],
        limit: int,
    ) -> ProviderSearchResponse: ...

    async def lookup_by_url(self, url: str) -> SearchResult | None:
        """Resolve a provider URL to a single SearchResult; None if URL not handled."""
        ...


@runtime_checkable
class ArtworkResolver(Protocol):
    """Best-effort cover-art back-fill for results a provider returned art-less.

    Used by SearchMusic to enrich merged results whose image_url is None
    (MusicBrainz items, iTunes artists). Returns an image URL or None; never
    raises (the search must not fail because art lookup failed).
    """

    async def resolve_artwork(
        self,
        kind: ResultKind,
        title: str,
        subtitle: str | None,
    ) -> str | None: ...


@runtime_checkable
class PopularityResolver(Protocol):
    """Best-effort cross-source popularity back-fill, keyed by (artist, title).

    Used by SearchMusic to enrich every top result with a uniform popularity
    signal (e.g. Last.fm getInfo play counts), regardless of which provider
    surfaced it — so mainstream and underground rank on the same basis.
    Returns a normalized value in [0, 1], or None when unknown. Never raises.
    """

    async def resolve_popularity(
        self,
        kind: ResultKind,
        title: str,
        subtitle: str | None,
    ) -> float | None: ...


@runtime_checkable
class QueryCache(Protocol):
    """Per-source post-ACL SearchResult cache.

    Per ADR-0007 §3.4: keyed discovery:v1:<source>:<kinds_csv>:<sha256>.
    """

    async def get(
        self,
        provider: str,
        query_norm: str,
        kinds: frozenset[ResultKind],
    ) -> tuple[tuple[SearchResult, ...], datetime] | None:
        """Return (results, fetched_at) if warm; None on miss or Redis error."""
        ...

    async def set(
        self,
        provider: str,
        query_norm: str,
        kinds: frozenset[ResultKind],
        results: tuple[SearchResult, ...],
        ttl: timedelta,
    ) -> None: ...


@runtime_checkable
class SearchHistoryRepository(Protocol):
    """Per-user search-history persistence (AC#11, 12, 13, 14)."""

    async def insert(self, entry: SearchHistoryEntry) -> None: ...

    async def trim_to_n(self, user_id: UserId, n: int) -> None:
        """Keep the latest n rows for the user; drop older ones."""
        ...

    async def list_distinct_recent(
        self, user_id: UserId, limit: int
    ) -> tuple[SearchHistoryEntry, ...]:
        """Return the latest distinct-by-query_norm entries for the user."""
        ...


@dataclass(frozen=True, slots=True)
class ClickInsertOutcome:
    """Result of insert_if_outside_window."""

    inserted: bool
    deduped_against_id: UUID | None


@runtime_checkable
class SearchClickRepository(Protocol):
    """Per-user click persistence with sliding-window dedup (AC#15, 16)."""

    async def insert_if_outside_window(
        self,
        click: SearchClick,
        window_seconds: int,
    ) -> ClickInsertOutcome: ...


# --- Catalog browse ports (view-result-detail extension, AC#14-20) ---


@dataclass(frozen=True, slots=True)
class ContentFetchResponse:
    """Response from album/artist content fetch.

    Status indicates the outcome; items is empty for non-OK statuses.
    """

    provider_name: str
    status: ProviderStatus
    items: tuple[SearchResult, ...]
    latency_ms: int


@runtime_checkable
class AlbumContentProvider(Protocol):
    """Fetch tracks from an album by provider + external ID.

    Adapter implementations live in adapters/outbound/discovery/<source>/.
    Supports Deezer, MusicBrainz, Last.fm; iTunes/TheAudioDB skipped (no ID lookups).
    """

    @property
    def name(self) -> str: ...

    async def get_album_tracks(
        self,
        external_id: str,
        limit: int,
    ) -> ContentFetchResponse: ...


@runtime_checkable
class ArtistContentProvider(Protocol):
    """Fetch top tracks and albums from an artist by provider + external ID.

    Adapter implementations live in adapters/outbound/discovery/<source>/.
    Supports Deezer, MusicBrainz, Last.fm; iTunes/TheAudioDB skipped.
    """

    @property
    def name(self) -> str: ...

    async def get_artist_top_tracks(
        self,
        external_id: str,
        limit: int,
    ) -> ContentFetchResponse: ...

    async def get_artist_albums(
        self,
        external_id: str,
        limit: int,
    ) -> ContentFetchResponse: ...

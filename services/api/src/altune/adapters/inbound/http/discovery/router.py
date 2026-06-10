# mypy: ignore_errors = True
"""Discovery HTTP router — GET /v1/discovery/search.

Slice 18. Validates the input via FastAPI Query bounds (422 on out-of-range
per AC#17), depends on current_user_id (401 on missing/invalid token per
AC#18), and delegates to SearchMusic via app.state-wired providers +
history repo.
"""

from __future__ import annotations

import structlog
from fastapi import APIRouter, Depends, HTTPException, Query, Request

from altune.adapters.inbound.http.discovery.dto import (
    CacheDto,
    ContentFetchResponseDto,
    DiscoveryClickRequest,
    DiscoverySearchHistoryResponse,
    DiscoverySearchResponse,
    ProviderStatusDto,
    SearchHistoryItemDto,
    SearchResultDto,
    SourceDto,
)
from altune.application.discovery.list_search_history import (
    ListSearchHistory,
    ListSearchHistoryInput,
)
from altune.application.discovery.record_click import RecordClick, RecordClickInput
from altune.application.discovery.search_music import (
    SearchMusic,
    SearchMusicInput,
)
from altune.domain.discovery.confidence import Confidence
from altune.domain.discovery.result_kind import ResultKind
from altune.domain.shared.user_id import UserId  # noqa: TC001
from altune.platform.auth import current_user_id

router = APIRouter(prefix="/v1/discovery", tags=["discovery"])
log = structlog.get_logger(__name__)

_ALL_KINDS = {k.value for k in ResultKind}


def _parse_kinds(raw: str) -> frozenset[ResultKind]:
    parts = [p.strip() for p in raw.split(",") if p.strip()]
    if not parts:
        raise HTTPException(status_code=422, detail="kinds must be non-empty")
    invalid = [p for p in parts if p not in _ALL_KINDS]
    if invalid:
        raise HTTPException(
            status_code=422,
            detail=f"kinds contains invalid values: {invalid}",
        )
    return frozenset(ResultKind(p) for p in parts)


@router.get(
    "/search",
    response_model=DiscoverySearchResponse,
)
async def get_discovery_search(
    request: Request,
    user_id: UserId = Depends(current_user_id),  # noqa: B008
    q: str = Query(..., min_length=1, max_length=200),
    kinds: str = Query(",".join(sorted(_ALL_KINDS))),
    limit: int = Query(25, ge=1, le=50),
    save_history: bool = Query(True),
) -> DiscoverySearchResponse:
    kinds_set = _parse_kinds(kinds)
    log.info(
        "discovery_search_request",
        user_id=str(user_id),
        q_len=len(q),
        kinds=sorted(k.value for k in kinds_set),
        limit=limit,
    )
    providers = getattr(request.app.state, "discovery_providers", ())
    cache = getattr(request.app.state, "discovery_cache", None)
    # Cover-art back-fill for art-less results (MusicBrainz items, iTunes
    # artists). Prefer the wired chained resolver (Deezer -> TheAudioDB); fall
    # back to the Deezer adapter when app.state has no resolver (e.g. tests).
    artwork_resolver = getattr(request.app.state, "discovery_artwork_resolver", None) or next(
        (p for p in providers if getattr(p, "name", None) == "deezer"), None
    )
    # Uniform popularity back-fill via Last.fm getInfo (wired in app.py).
    popularity_resolver = getattr(request.app.state, "discovery_popularity_resolver", None) or next(
        (p for p in providers if getattr(p, "name", None) == "lastfm"), None
    )
    quality_scorer = getattr(request.app.state, "discovery_quality_scorer", None)
    mbid_resolver = getattr(request.app.state, "discovery_mbid_resolver", None)
    content_validation_cache = getattr(
        request.app.state, "discovery_content_validation_cache", None
    )
    fanart_resolver = getattr(request.app.state, "discovery_fanart_resolver", None)
    genius_resolver = getattr(request.app.state, "discovery_genius_resolver", None)
    track_title_source = getattr(request.app.state, "discovery_track_title_source", None)
    sessionmaker = getattr(request.app.state, "sessionmaker", None)
    if sessionmaker is not None:
        from altune.adapters.outbound.persistence.discovery.search_history_repository import (
            SqlAlchemySearchHistoryRepository,
        )

        async with sessionmaker() as session:
            repo = SqlAlchemySearchHistoryRepository(session)
            use_case = SearchMusic(
                providers=providers,
                history_repo=repo,
                cache=cache,
                artwork_resolver=artwork_resolver,
                popularity_resolver=popularity_resolver,
                quality_scorer=quality_scorer,
                mbid_resolver=mbid_resolver,
                content_validation_cache=content_validation_cache,
                fanart_resolver=fanart_resolver,
                genius_resolver=genius_resolver,
                track_title_source=track_title_source,
            )
            output = await use_case.execute(
                SearchMusicInput(
                    raw_query=q,
                    user_id=user_id,
                    kinds=kinds_set,
                    limit=limit,
                    save_history=save_history,
                )
            )
            await session.commit()
    else:
        # Fallback path for environments without persistence (tests, smoke).
        history_repo = request.app.state.discovery_history_repo
        use_case = SearchMusic(
            providers=providers,
            history_repo=history_repo,
            cache=cache,
            artwork_resolver=artwork_resolver,
            popularity_resolver=popularity_resolver,
            quality_scorer=quality_scorer,
            mbid_resolver=mbid_resolver,
            content_validation_cache=content_validation_cache,
            fanart_resolver=fanart_resolver,
            genius_resolver=genius_resolver,
            track_title_source=track_title_source,
        )
        output = await use_case.execute(
            SearchMusicInput(
                raw_query=q,
                user_id=user_id,
                kinds=kinds_set,
                limit=limit,
            )
        )
    return DiscoverySearchResponse(
        query=output.query,
        query_norm=output.query_norm,
        results=[
            SearchResultDto(
                kind=r.kind.value,
                title=r.title,
                subtitle=r.subtitle,
                image_url=r.image_url,
                confidence=r.confidence.value,
                sources=[
                    SourceDto(
                        provider=s.provider.value,
                        external_id=s.external_id,
                        url=s.url,
                    )
                    for s in r.sources
                ],
                extras=dict(r.extras),
            )
            for r in output.results
        ],
        providers=[
            ProviderStatusDto(
                provider=p.provider_name,
                status=p.status.value,
                result_count=p.result_count,
                latency_ms=p.latency_ms,
            )
            for p in output.providers
        ],
        partial=output.partial,
        cache=CacheDto(hit=output.cache_hit, fetched_at=output.cache_fetched_at),
    )


@router.get(
    "/search-history",
    response_model=DiscoverySearchHistoryResponse,
)
async def get_discovery_search_history(
    request: Request,
    user_id: UserId = Depends(current_user_id),  # noqa: B008
    limit: int = Query(10, ge=1, le=50),
) -> DiscoverySearchHistoryResponse:
    sessionmaker = getattr(request.app.state, "sessionmaker", None)
    if sessionmaker is not None:
        from altune.adapters.outbound.persistence.discovery.search_history_repository import (
            SqlAlchemySearchHistoryRepository,
        )

        async with sessionmaker() as session:
            repo = SqlAlchemySearchHistoryRepository(session)
            use_case = ListSearchHistory(history_repo=repo)
            output = await use_case.execute(ListSearchHistoryInput(user_id=user_id, limit=limit))
    else:
        history_repo = request.app.state.discovery_history_repo
        use_case = ListSearchHistory(history_repo=history_repo)
        output = await use_case.execute(ListSearchHistoryInput(user_id=user_id, limit=limit))
    return DiscoverySearchHistoryResponse(
        items=[
            SearchHistoryItemDto(
                query=e.query,
                query_norm=e.query_norm,
                executed_at=e.executed_at,
            )
            for e in output.items
        ],
        total=output.total,
    )


@router.post("/clicks", status_code=202)
async def post_discovery_click(
    request: Request,
    body: DiscoveryClickRequest,
    user_id: UserId = Depends(current_user_id),  # noqa: B008
) -> None:
    # Validate kind + confidence at the application boundary.
    try:
        kind = ResultKind(body.kind)
    except ValueError as exc:
        raise HTTPException(status_code=422, detail=f"invalid kind: {body.kind}") from exc
    try:
        confidence = Confidence(body.confidence)
    except ValueError as exc:
        raise HTTPException(
            status_code=422, detail=f"invalid confidence: {body.confidence}"
        ) from exc
    if body.position < 0:
        raise HTTPException(status_code=422, detail="position must be non-negative")
    sessionmaker = getattr(request.app.state, "sessionmaker", None)
    if sessionmaker is None:
        # No DB configured (smoke env); accept-and-drop.
        return None
    from altune.adapters.outbound.persistence.discovery.search_click_repository import (
        SqlAlchemySearchClickRepository,
    )

    async with sessionmaker() as session:
        repo = SqlAlchemySearchClickRepository(session)
        use_case = RecordClick(click_repo=repo)
        await use_case.execute(
            RecordClickInput(
                user_id=user_id,
                query_norm=body.query_norm,
                kind=kind,
                title=body.title,
                subtitle=body.subtitle,
                position=body.position,
                confidence=confidence,
            )
        )
        await session.commit()
    return None


# --- Catalog browse routes (AC#14-20) ---


def _result_to_dto(r: SearchResult) -> SearchResultDto:
    """Convert a domain SearchResult to wire DTO."""

    return SearchResultDto(
        kind=r.kind.value,
        title=r.title,
        subtitle=r.subtitle,
        image_url=r.image_url,
        confidence=r.confidence.value,
        sources=[
            SourceDto(
                provider=s.provider.value,
                external_id=s.external_id,
                url=s.url,
            )
            for s in r.sources
        ],
        extras=dict(r.extras),
    )


@router.get(
    "/albums/{provider}/{external_id}/tracks",
    response_model=ContentFetchResponseDto,
)
async def get_album_tracks(
    request: Request,
    provider: str,
    external_id: str,
    _user_id: UserId = Depends(current_user_id),  # noqa: B008
    limit: int = Query(50, ge=1, le=100),
) -> ContentFetchResponseDto:
    """Fetch tracks from an album by provider + external ID (AC#14)."""
    from altune.application.discovery.get_album_tracks import (
        GetAlbumTracks,
        GetAlbumTracksInput,
    )
    from altune.application.discovery.ports import AlbumContentProvider

    # Build provider map from app.state.discovery_providers
    providers = getattr(request.app.state, "discovery_providers", ())
    content_providers: dict[str, AlbumContentProvider] = {
        p.name: p for p in providers if hasattr(p, "get_album_tracks")
    }

    use_case = GetAlbumTracks(providers=content_providers)
    output = await use_case.execute(
        GetAlbumTracksInput(provider=provider, external_id=external_id, limit=limit)
    )

    return ContentFetchResponseDto(
        items=[_result_to_dto(r) for r in output.items],
        provider=output.provider_name,
        status=output.status.value,
        latency_ms=output.latency_ms,
    )


@router.get(
    "/artists/{provider}/{external_id}/top-tracks",
    response_model=ContentFetchResponseDto,
)
async def get_artist_top_tracks(
    request: Request,
    provider: str,
    external_id: str,
    _user_id: UserId = Depends(current_user_id),  # noqa: B008
    limit: int = Query(5, ge=1, le=20),
) -> ContentFetchResponseDto:
    """Fetch top tracks from an artist by provider + external ID (AC#17)."""
    from altune.application.discovery.get_artist_content import (
        GetArtistTopTracks,
        GetArtistTopTracksInput,
    )
    from altune.application.discovery.ports import ArtistContentProvider

    providers = getattr(request.app.state, "discovery_providers", ())
    content_providers: dict[str, ArtistContentProvider] = {
        p.name: p for p in providers if hasattr(p, "get_artist_top_tracks")
    }

    use_case = GetArtistTopTracks(providers=content_providers)
    output = await use_case.execute(
        GetArtistTopTracksInput(provider=provider, external_id=external_id, limit=limit)
    )

    return ContentFetchResponseDto(
        items=[_result_to_dto(r) for r in output.items],
        provider=output.provider_name,
        status=output.status.value,
        latency_ms=output.latency_ms,
    )


@router.get(
    "/artists/{provider}/{external_id}/albums",
    response_model=ContentFetchResponseDto,
)
async def get_artist_albums(
    request: Request,
    provider: str,
    external_id: str,
    _user_id: UserId = Depends(current_user_id),  # noqa: B008
    limit: int = Query(10, ge=1, le=100),
) -> ContentFetchResponseDto:
    """Fetch albums from an artist by provider + external ID (AC#18)."""
    from altune.application.discovery.get_artist_content import (
        GetArtistAlbums,
        GetArtistAlbumsInput,
    )
    from altune.application.discovery.ports import ArtistContentProvider

    providers = getattr(request.app.state, "discovery_providers", ())
    content_providers: dict[str, ArtistContentProvider] = {
        p.name: p for p in providers if hasattr(p, "get_artist_albums")
    }

    use_case = GetArtistAlbums(providers=content_providers)
    output = await use_case.execute(
        GetArtistAlbumsInput(provider=provider, external_id=external_id, limit=limit)
    )

    return ContentFetchResponseDto(
        items=[_result_to_dto(r) for r in output.items],
        provider=output.provider_name,
        status=output.status.value,
        latency_ms=output.latency_ms,
    )

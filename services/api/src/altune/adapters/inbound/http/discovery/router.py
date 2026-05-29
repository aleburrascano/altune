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
    # Reuse the Deezer adapter (no-auth, has artwork) to back-fill covers for
    # art-less results (MusicBrainz items, iTunes artists).
    artwork_resolver = next((p for p in providers if getattr(p, "name", None) == "deezer"), None)
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
            )
            output = await use_case.execute(
                SearchMusicInput(
                    raw_query=q,
                    user_id=user_id,
                    kinds=kinds_set,
                    limit=limit,
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

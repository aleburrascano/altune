"""Catalog HTTP router — GET /v1/tracks.

Per the spec. The router is a thin shell: FastAPI Query bounds handle 422,
the auth dep resolves the current user, a session is acquired from
app.state.sessionmaker (set up by the platform/app.py lifespan), and the
ListTracks use case does the actual work. TrackRow never leaves this layer;
the response is built from domain Track values.
"""

from __future__ import annotations

import structlog
from fastapi import APIRouter, Depends, Query, Request

from altune.adapters.inbound.http.catalog.dto import ListTracksResponse, TrackResponse
from altune.adapters.outbound.persistence.catalog.track_repository import (
    SqlAlchemyTrackRepository,
)
from altune.application.catalog.list_tracks import ListTracks, ListTracksInput
from altune.domain.shared.user_id import UserId  # noqa: TC001  # FastAPI runtime annotation
from altune.platform.auth import current_user_id

router = APIRouter(prefix="/v1", tags=["tracks"])
log = structlog.get_logger(__name__)


@router.get("/tracks", response_model=ListTracksResponse)  # type: ignore[untyped-decorator, unused-ignore]
async def get_tracks(
    request: Request,
    user_id: UserId = Depends(current_user_id),  # noqa: B008  # FastAPI dependency injection idiom
    limit: int = Query(50, ge=1, le=200),
    offset: int = Query(0, ge=0),
) -> ListTracksResponse:
    log.info("http_get_tracks_request", user_id=str(user_id), limit=limit, offset=offset)
    sessionmaker = request.app.state.sessionmaker
    async with sessionmaker() as session:
        repo = SqlAlchemyTrackRepository(session)
        use_case = ListTracks(repo)
        output = await use_case.execute(
            ListTracksInput(user_id=user_id, limit=limit, offset=offset)
        )
    return ListTracksResponse(
        items=[
            TrackResponse(
                id=t.id.value,
                title=t.title,
                artist=t.artist,
                album=t.album,
                duration_seconds=t.duration_seconds,
                added_at=t.added_at,
            )
            for t in output.items
        ],
        total=output.total,
        limit=output.limit,
        offset=output.offset,
        has_more=output.has_more,
    )

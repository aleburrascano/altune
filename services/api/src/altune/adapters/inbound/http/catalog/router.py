"""Catalog HTTP router — GET /v1/tracks.

Per the spec. The router is a 5-line shell: parse query params (FastAPI does
the 422 work via Query bounds), resolve current_user_id from the auth dep,
acquire a session from app.state.sessionmaker, call ListTracks, serialize.

STUB: GREEN commit implements the real wiring. Currently returns an empty
response so the e2e tests fail meaningfully on happy/isolation paths while
the 422 tests pass via FastAPI's Query validation.
"""

from __future__ import annotations

from typing import TYPE_CHECKING

import structlog
from fastapi import APIRouter, Depends, Query

from altune.adapters.inbound.http.catalog.dto import ListTracksResponse
from altune.platform.auth import current_user_id

if TYPE_CHECKING:
    from altune.domain.shared.user_id import UserId

router = APIRouter(prefix="/v1", tags=["tracks"])
log = structlog.get_logger(__name__)


@router.get("/tracks", response_model=ListTracksResponse)  # type: ignore[untyped-decorator, unused-ignore]
async def get_tracks(
    user_id: UserId = Depends(current_user_id),  # noqa: B008  # FastAPI dependency injection idiom
    limit: int = Query(50, ge=1, le=200),
    offset: int = Query(0, ge=0),
) -> ListTracksResponse:
    log.info("http_get_tracks_request", user_id=str(user_id), limit=limit, offset=offset)
    # STUB
    return ListTracksResponse(items=[], total=0, limit=limit, offset=offset, has_more=False)

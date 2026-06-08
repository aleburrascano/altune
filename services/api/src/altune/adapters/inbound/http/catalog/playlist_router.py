"""Playlist HTTP routes — CRUD + track management."""

from __future__ import annotations

from datetime import datetime
from uuid import UUID

import structlog
from fastapi import APIRouter, Depends, Request, Response

from altune.adapters.inbound.http.catalog.dto import (
    AddTrackToPlaylistRequest,
    CreatePlaylistRequest,
    ListPlaylistsResponse,
    PlaylistDetailResponse,
    PlaylistResponse,
    RenamePlaylistRequest,
    ReorderTracksRequest,
    TrackResponse,
)
from altune.adapters.inbound.http.catalog.router import _track_response
from altune.adapters.outbound.persistence.catalog.playlist_repository import (
    SqlAlchemyPlaylistRepository,
)
from altune.application.catalog.add_track_to_playlist import (
    AddTrackToPlaylist,
    AddTrackToPlaylistInput,
)
from altune.application.catalog.create_playlist import CreatePlaylist, CreatePlaylistInput
from altune.application.catalog.delete_playlist import DeletePlaylist, DeletePlaylistInput
from altune.application.catalog.get_playlist import GetPlaylist, GetPlaylistInput
from altune.application.catalog.list_playlists import ListPlaylists, ListPlaylistsInput
from altune.application.catalog.remove_track_from_playlist import (
    RemoveTrackFromPlaylist,
    RemoveTrackFromPlaylistInput,
)
from altune.application.catalog.rename_playlist import RenamePlaylist, RenamePlaylistInput
from altune.application.catalog.reorder_playlist_tracks import (
    ReorderPlaylistTracks,
    ReorderPlaylistTracksInput,
)
from altune.domain.catalog.playlist_id import PlaylistId
from altune.domain.catalog.track_id import TrackId
from altune.domain.shared.user_id import UserId  # noqa: TC001
from altune.platform.auth import current_user_id

router = APIRouter(prefix="/v1", tags=["playlists"])
log = structlog.get_logger(__name__)


async def _playlist_response(
    repo: SqlAlchemyPlaylistRepository,
    playlist_id: PlaylistId,
    user_id: UserId,
) -> PlaylistResponse:
    from altune.domain.catalog.playlist import Playlist

    pl = await repo.get_by_id(playlist_id, user_id)
    assert pl is not None
    artwork = await repo.get_preview_artwork(playlist_id, user_id)
    count = await repo.get_track_count(playlist_id)
    return PlaylistResponse(
        id=pl.id.value,
        name=pl.name,
        track_count=count,
        preview_artwork_urls=list(artwork),
        created_at=pl.created_at,
        updated_at=pl.updated_at,
    )


@router.post("/playlists", response_model=PlaylistResponse, status_code=201)  # type: ignore[untyped-decorator, unused-ignore]
async def create_playlist(
    body: CreatePlaylistRequest,
    request: Request,
    user_id: UserId = Depends(current_user_id),  # noqa: B008
) -> PlaylistResponse:
    sessionmaker = request.app.state.sessionmaker
    async with sessionmaker() as session:
        repo = SqlAlchemyPlaylistRepository(session)
        use_case = CreatePlaylist(repo)
        playlist = await use_case.execute(CreatePlaylistInput(user_id=user_id, name=body.name))
        await session.commit()
    return PlaylistResponse(
        id=playlist.id.value,
        name=playlist.name,
        track_count=0,
        preview_artwork_urls=[],
        created_at=playlist.created_at,
        updated_at=playlist.updated_at,
    )


@router.get("/playlists", response_model=ListPlaylistsResponse)  # type: ignore[untyped-decorator, unused-ignore]
async def list_playlists(
    request: Request,
    user_id: UserId = Depends(current_user_id),  # noqa: B008
) -> ListPlaylistsResponse:
    sessionmaker = request.app.state.sessionmaker
    async with sessionmaker() as session:
        repo = SqlAlchemyPlaylistRepository(session)
        use_case = ListPlaylists(repo)
        output = await use_case.execute(ListPlaylistsInput(user_id=user_id))

        items: list[PlaylistResponse] = []
        for pl in output.items:
            artwork = await repo.get_preview_artwork(pl.id, user_id)
            count = await repo.get_track_count(pl.id)
            items.append(
                PlaylistResponse(
                    id=pl.id.value,
                    name=pl.name,
                    track_count=count,
                    preview_artwork_urls=list(artwork),
                    created_at=pl.created_at,
                    updated_at=pl.updated_at,
                )
            )
    return ListPlaylistsResponse(items=items, total=len(items))


@router.get("/playlists/{playlist_id}", response_model=PlaylistDetailResponse)  # type: ignore[untyped-decorator, unused-ignore]
async def get_playlist(
    playlist_id: UUID,
    request: Request,
    response: Response,
    user_id: UserId = Depends(current_user_id),  # noqa: B008
) -> PlaylistDetailResponse:
    sessionmaker = request.app.state.sessionmaker
    async with sessionmaker() as session:
        repo = SqlAlchemyPlaylistRepository(session)
        use_case = GetPlaylist(repo)
        output = await use_case.execute(
            GetPlaylistInput(playlist_id=PlaylistId(playlist_id), user_id=user_id),
        )
    if output is None:
        response.status_code = 404
        return PlaylistDetailResponse(
            id=playlist_id,
            name="",
            track_count=0,
            preview_artwork_urls=[],
            created_at=datetime_stub(),
            updated_at=datetime_stub(),
            tracks=[],
        )
    artwork: list[str] = []
    seen: set[str] = set()
    for t in output.tracks:
        if t.artwork_url and t.artwork_url not in seen and len(artwork) < 4:
            artwork.append(t.artwork_url)
            seen.add(t.artwork_url)
    return PlaylistDetailResponse(
        id=output.playlist.id.value,
        name=output.playlist.name,
        track_count=len(output.tracks),
        preview_artwork_urls=artwork,
        created_at=output.playlist.created_at,
        updated_at=output.playlist.updated_at,
        tracks=[_track_response(t) for t in output.tracks],
    )


@router.patch("/playlists/{playlist_id}", response_model=PlaylistResponse)  # type: ignore[untyped-decorator, unused-ignore]
async def rename_playlist(
    playlist_id: UUID,
    body: RenamePlaylistRequest,
    request: Request,
    response: Response,
    user_id: UserId = Depends(current_user_id),  # noqa: B008
) -> PlaylistResponse:
    sessionmaker = request.app.state.sessionmaker
    async with sessionmaker() as session:
        repo = SqlAlchemyPlaylistRepository(session)
        use_case = RenamePlaylist(repo)
        result = await use_case.execute(
            RenamePlaylistInput(
                playlist_id=PlaylistId(playlist_id), user_id=user_id, name=body.name
            ),
        )
        if result is None:
            await session.commit()
            response.status_code = 404
            return PlaylistResponse(
                id=playlist_id,
                name="",
                track_count=0,
                preview_artwork_urls=[],
                created_at=datetime_stub(),
                updated_at=datetime_stub(),
            )
        count = await repo.get_track_count(result.id)
        await session.commit()
    return PlaylistResponse(
        id=result.id.value,
        name=result.name,
        track_count=count,
        preview_artwork_urls=[],
        created_at=result.created_at,
        updated_at=result.updated_at,
    )


@router.delete("/playlists/{playlist_id}", status_code=204)  # type: ignore[untyped-decorator, unused-ignore]
async def delete_playlist(
    playlist_id: UUID,
    request: Request,
    response: Response,
    user_id: UserId = Depends(current_user_id),  # noqa: B008
) -> None:
    sessionmaker = request.app.state.sessionmaker
    async with sessionmaker() as session:
        repo = SqlAlchemyPlaylistRepository(session)
        use_case = DeletePlaylist(repo)
        deleted = await use_case.execute(
            DeletePlaylistInput(playlist_id=PlaylistId(playlist_id), user_id=user_id),
        )
        await session.commit()
    if not deleted:
        response.status_code = 404


@router.post("/playlists/{playlist_id}/tracks", status_code=201)  # type: ignore[untyped-decorator, unused-ignore]
async def add_track_to_playlist(
    playlist_id: UUID,
    body: AddTrackToPlaylistRequest,
    request: Request,
    response: Response,
    user_id: UserId = Depends(current_user_id),  # noqa: B008
) -> None:
    sessionmaker = request.app.state.sessionmaker
    async with sessionmaker() as session:
        repo = SqlAlchemyPlaylistRepository(session)
        use_case = AddTrackToPlaylist(repo)
        added = await use_case.execute(
            AddTrackToPlaylistInput(
                playlist_id=PlaylistId(playlist_id),
                user_id=user_id,
                track_id=TrackId(body.track_id),
            ),
        )
        await session.commit()
    if not added:
        response.status_code = 409


@router.delete("/playlists/{playlist_id}/tracks/{track_id}", status_code=204)  # type: ignore[untyped-decorator, unused-ignore]
async def remove_track_from_playlist(
    playlist_id: UUID,
    track_id: UUID,
    request: Request,
    response: Response,
    user_id: UserId = Depends(current_user_id),  # noqa: B008
) -> None:
    sessionmaker = request.app.state.sessionmaker
    async with sessionmaker() as session:
        repo = SqlAlchemyPlaylistRepository(session)
        use_case = RemoveTrackFromPlaylist(repo)
        removed = await use_case.execute(
            RemoveTrackFromPlaylistInput(
                playlist_id=PlaylistId(playlist_id),
                user_id=user_id,
                track_id=TrackId(track_id),
            ),
        )
        await session.commit()
    if not removed:
        response.status_code = 404


@router.patch("/playlists/{playlist_id}/tracks/reorder", status_code=200)  # type: ignore[untyped-decorator, unused-ignore]
async def reorder_playlist_tracks(
    playlist_id: UUID,
    body: ReorderTracksRequest,
    request: Request,
    response: Response,
    user_id: UserId = Depends(current_user_id),  # noqa: B008
) -> None:
    sessionmaker = request.app.state.sessionmaker
    async with sessionmaker() as session:
        repo = SqlAlchemyPlaylistRepository(session)
        use_case = ReorderPlaylistTracks(repo)
        reordered = await use_case.execute(
            ReorderPlaylistTracksInput(
                playlist_id=PlaylistId(playlist_id),
                user_id=user_id,
                track_ids=[TrackId(tid) for tid in body.track_ids],
            ),
        )
        await session.commit()
    if not reordered:
        response.status_code = 404


def datetime_stub() -> datetime:
    from datetime import UTC

    return datetime(2000, 1, 1, tzinfo=UTC)

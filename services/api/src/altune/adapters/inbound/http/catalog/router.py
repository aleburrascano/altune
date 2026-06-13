"""Catalog HTTP router — GET /v1/tracks.

Per the spec. The router is a thin shell: FastAPI Query bounds handle 422,
the auth dep resolves the current user, a session is acquired from
app.state.sessionmaker (set up by the platform/app.py lifespan), and the
ListTracks use case does the actual work. TrackRow never leaves this layer;
the response is built from domain Track values.
"""

from pathlib import Path
from uuid import UUID  # used as runtime annotation by FastAPI

import structlog
from fastapi import APIRouter, BackgroundTasks, Depends, Query, Request, Response
from fastapi.responses import FileResponse

from altune.adapters.inbound.http.catalog.dto import (
    CreateTrackRequest,
    ListTracksResponse,
    TrackResponse,
)
from altune.adapters.outbound.persistence.catalog.track_repository import (
    SqlAlchemyTrackRepository,
)
from altune.application.catalog.add_track_to_library import (
    AddTrackToLibrary,
    AddTrackToLibraryInput,
)
from altune.application.catalog.list_tracks import ListTracks, ListTracksInput
from altune.domain.catalog.track import Track  # used at runtime
from altune.domain.shared.user_id import UserId  # FastAPI runtime annotation
from altune.platform.auth import current_user_id

router = APIRouter(prefix="/v1", tags=["tracks"])
log = structlog.get_logger(__name__)


def _track_response(t: Track) -> TrackResponse:
    return TrackResponse(
        id=t.id.value,
        title=t.title,
        artist=t.artist,
        album=t.album,
        duration_seconds=t.duration_seconds,
        added_at=t.added_at,
        acquisition_status=t.acquisition_status.value,
        artwork_url=t.artwork_url,
        year=t.year,
        genre=t.genre,
        track_number=t.track_number,
        album_artist=t.album_artist,
        isrc=t.isrc,
        audio_ref=t.audio_ref,
        failure_reason=t.failure_reason,
    )


@router.get("/tracks", response_model=ListTracksResponse)  # type: ignore[untyped-decorator, unused-ignore]
async def get_tracks(
    request: Request,
    user_id: UserId = Depends(current_user_id),  # noqa: B008  # FastAPI dependency injection idiom
    limit: int = Query(50, ge=1, le=2000),
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
        items=[_track_response(t) for t in output.items],
        total=output.total,
        limit=output.limit,
        offset=output.offset,
        has_more=output.has_more,
    )


@router.post("/tracks", response_model=TrackResponse, status_code=201)  # type: ignore[untyped-decorator, unused-ignore]
async def create_track(
    body: CreateTrackRequest,
    response: Response,
    request: Request,
    background_tasks: BackgroundTasks,
    user_id: UserId = Depends(current_user_id),  # noqa: B008  # FastAPI dependency injection idiom
) -> TrackResponse:
    log.info("http_post_tracks_request", user_id=str(user_id), title=body.title)
    sessionmaker = request.app.state.sessionmaker
    async with sessionmaker() as session:
        repo = SqlAlchemyTrackRepository(session)
        use_case = AddTrackToLibrary(repo)
        output = await use_case.execute(
            AddTrackToLibraryInput(
                user_id=user_id,
                title=body.title,
                artist=body.artist,
                album=body.album,
                duration_seconds=body.duration_seconds,
                artwork_url=body.artwork_url,
                isrc=body.isrc,
                year=body.year,
                genre=body.genre,
                album_artist=body.album_artist,
            )
        )
        await session.commit()
    if output.created:
        _schedule_acquisition(request, background_tasks, output.track.id, user_id)
    else:
        response.status_code = 200
        log.info(
            "http_post_tracks_dedup_hit", user_id=str(user_id), track_id=str(output.track.id.value)
        )
        if output.track.acquisition_status.value in ("failed", "pending", "ready"):
            _schedule_acquisition(request, background_tasks, output.track.id, user_id)
    return _track_response(output.track)


async def _run_acquisition(
    sessionmaker: object,
    searcher: object,
    store: object,
    track_id: object,
    user_id: object,
) -> None:
    from altune.application.catalog.acquisition.acquire_track_audio import AcquireTrackAudio
    from altune.domain.catalog.track_id import TrackId
    from altune.domain.shared.user_id import UserId as UID

    assert isinstance(track_id, TrackId)
    assert isinstance(user_id, UID)
    async with sessionmaker() as session:  # type: ignore[operator]
        repo = SqlAlchemyTrackRepository(session)
        use_case = AcquireTrackAudio(repo, searcher, store)  # type: ignore[arg-type]
        await use_case.execute(track_id, user_id)
        await session.commit()


def _schedule_acquisition(
    request: Request,
    background_tasks: BackgroundTasks,
    track_id: object,
    user_id: object,
) -> None:
    searcher = getattr(request.app.state, "audio_searcher", None)
    store = getattr(request.app.state, "audio_store", None)
    sessionmaker = getattr(request.app.state, "sessionmaker", None)
    if searcher is None or store is None or sessionmaker is None:
        log.warning("acquisition_not_configured")
        return
    background_tasks.add_task(_run_acquisition, sessionmaker, searcher, store, track_id, user_id)


@router.delete("/tracks/{track_id}", status_code=204)  # type: ignore[untyped-decorator, unused-ignore]
async def delete_track(
    track_id: UUID,
    request: Request,
    user_id: UserId = Depends(current_user_id),  # noqa: B008
) -> Response:
    from altune.domain.catalog.track_id import TrackId

    log.info("http_delete_track_request", user_id=str(user_id), track_id=str(track_id))
    sessionmaker = request.app.state.sessionmaker
    async with sessionmaker() as session:
        repo = SqlAlchemyTrackRepository(session)
        deleted = await repo.delete(TrackId(track_id), user_id)
        if deleted:
            await session.commit()
            log.info("track_deleted", user_id=str(user_id), track_id=str(track_id))
        else:
            log.info("track_delete_not_found", user_id=str(user_id), track_id=str(track_id))
    return Response(status_code=204 if deleted else 404)


@router.get("/tracks/{track_id}/audio")  # type: ignore[untyped-decorator, unused-ignore]
async def stream_audio(
    track_id: UUID,
    request: Request,
    user_id: UserId = Depends(current_user_id),  # noqa: B008
) -> FileResponse:
    from altune.domain.catalog.acquisition_status import AcquisitionStatus
    from altune.domain.catalog.track_id import TrackId

    sessionmaker = request.app.state.sessionmaker
    async with sessionmaker() as session:
        repo = SqlAlchemyTrackRepository(session)
        track = await repo.get_by_id(TrackId(track_id), user_id)

    if track is None or track.acquisition_status != AcquisitionStatus.READY or not track.audio_ref:
        return Response(status_code=404)  # type: ignore[return-value]

    base_dir: str | None = getattr(request.app.state, "music_dir", None)
    if base_dir is None:
        return Response(status_code=404)  # type: ignore[return-value]

    file_path = Path(base_dir) / track.audio_ref
    if not file_path.is_file():
        log.warning(
            "audio_file_missing",
            track_id=str(track_id),
            audio_ref=track.audio_ref,
            user_id=str(user_id),
        )
        from altune.application.catalog.reconcile_track_status import (
            ReconcileInput,
            ReconcileTrackStatus,
        )

        audio_store = getattr(request.app.state, "audio_store", None)
        if audio_store is not None:
            async with sessionmaker() as write_session:
                write_repo = SqlAlchemyTrackRepository(write_session)
                use_case = ReconcileTrackStatus(write_repo, audio_store)
                result = await use_case.execute(
                    ReconcileInput(
                        track_id=TrackId(track_id),
                        user_id=user_id,
                        reason="Audio file missing from storage",
                    ),
                )
                await write_session.commit()
            if result.event:
                log.info(
                    "track_marked_failed",
                    track_id=str(track_id),
                    reason="audio_file_missing",
                )
        return Response(status_code=404)  # type: ignore[return-value]

    log.info(
        "audio_stream_started",
        track_id=str(track_id),
        user_id=str(user_id),
        file_size_bytes=file_path.stat().st_size,
    )
    return FileResponse(
        path=file_path,
        media_type="audio/mpeg",
        headers={"Cache-Control": "private, max-age=86400"},
    )

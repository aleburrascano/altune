"""SqlAlchemyTrackRepository — outbound persistence adapter for Track.

Implements `altune.application.catalog.ports.TrackRepository` against a real
Postgres via an `AsyncSession`. The session is supplied by the FastAPI
dependency in `adapters/inbound/http/catalog/` and disposed via the lifespan
in `platform/app.py`.
"""

from __future__ import annotations

from typing import TYPE_CHECKING

from sqlalchemy import func, select
from sqlalchemy.dialects.postgresql import insert as pg_insert

from altune.adapters.outbound.persistence.catalog.track_row import TrackRow
from altune.domain.catalog.dedup import dedup_key

if TYPE_CHECKING:
    from collections.abc import Sequence

    from sqlalchemy.ext.asyncio import AsyncSession

    from altune.domain.catalog.track import Track
    from altune.domain.catalog.track_id import TrackId
    from altune.domain.shared.user_id import UserId


class SqlAlchemyTrackRepository:
    def __init__(self, session: AsyncSession) -> None:
        self._session = session

    async def get_by_id(self, track_id: TrackId, user_id: UserId) -> Track | None:
        stmt = select(TrackRow).where(
            TrackRow.id == track_id.value,
            TrackRow.user_id == user_id.value,
        )
        result = await self._session.execute(stmt)
        row = result.scalar_one_or_none()
        return row.to_domain() if row else None

    async def update(self, track: Track) -> Track:
        stmt = select(TrackRow).where(TrackRow.id == track.id.value)
        result = await self._session.execute(stmt)
        row = result.scalar_one_or_none()
        if row is None:
            msg = f"Track {track.id} not found"
            raise ValueError(msg)
        row.title = track.title
        row.artist = track.artist
        row.album = track.album
        row.duration_seconds = track.duration_seconds
        row.artwork_url = track.artwork_url
        row.acquisition_status = track.acquisition_status.value
        row.year = track.year
        row.genre = track.genre
        row.track_number = track.track_number
        row.album_artist = track.album_artist
        row.isrc = track.isrc
        row.audio_ref = track.audio_ref
        row.failure_reason = track.failure_reason
        await self._session.flush()
        return row.to_domain()

    async def delete(self, track_id: TrackId, user_id: UserId) -> bool:
        stmt = select(TrackRow).where(
            TrackRow.id == track_id.value,
            TrackRow.user_id == user_id.value,
        )
        result = await self._session.execute(stmt)
        row = result.scalar_one_or_none()
        if row is None:
            return False
        await self._session.delete(row)
        await self._session.flush()
        return True

    async def add(self, track: Track) -> tuple[Track, bool]:
        # Natural idempotency: INSERT ... ON CONFLICT (user_id, dedup_key) DO
        # NOTHING RETURNING id. A returned id means we inserted (created); an
        # empty result means the row already existed. Either way we SELECT the
        # canonical row back to return it.
        key = dedup_key(track.title, track.artist, track.album)
        insert_stmt = (
            pg_insert(TrackRow)
            .values(
                id=track.id.value,
                user_id=track.user_id.value,
                title=track.title,
                artist=track.artist,
                album=track.album,
                duration_seconds=track.duration_seconds,
                added_at=track.added_at,
                artwork_url=track.artwork_url,
                acquisition_status=track.acquisition_status.value,
                dedup_key=key,
            )
            .on_conflict_do_nothing(index_elements=["user_id", "dedup_key"])
            .returning(TrackRow.id)
        )
        inserted = await self._session.execute(insert_stmt)
        created = inserted.scalar_one_or_none() is not None

        existing = await self._session.execute(
            select(TrackRow).where(
                TrackRow.user_id == track.user_id.value,
                TrackRow.dedup_key == key,
            )
        )
        row = existing.scalar_one()
        return row.to_domain(), created

    async def list_for_user(
        self,
        user_id: UserId,
        limit: int,
        offset: int,
    ) -> tuple[Sequence[Track], int]:
        # Page: WHERE user_id = $1 ORDER BY added_at DESC, id DESC LIMIT $2 OFFSET $3.
        # id-as-tiebreaker matches the index tracks_user_added_idx from the slice 4
        # migration and the deterministic order required by AC#1.
        page_stmt = (
            select(TrackRow)
            .where(TrackRow.user_id == user_id.value)
            .order_by(TrackRow.added_at.desc(), TrackRow.id.desc())
            .limit(limit)
            .offset(offset)
        )
        # Total: COUNT(*) of all the user's rows (not just this page). Spec accepts
        # per-request snapshot semantics; concurrent writes can shift `total` between
        # paged calls in production. v1 has no writers, so academic.
        count_stmt = (
            select(func.count()).select_from(TrackRow).where(TrackRow.user_id == user_id.value)
        )

        page_result = await self._session.execute(page_stmt)
        rows = page_result.scalars().all()
        count_result = await self._session.execute(count_stmt)
        total: int = count_result.scalar_one()

        items = tuple(row.to_domain() for row in rows)
        return items, total

"""SqlAlchemy implementation of the PlaylistRepository port."""

from __future__ import annotations

from collections.abc import Sequence
from datetime import UTC, datetime
from typing import TYPE_CHECKING

import sqlalchemy as sa
from sqlalchemy import delete, func, select

from altune.adapters.outbound.persistence.catalog.playlist_row import PlaylistRow, PlaylistTrackRow
from altune.adapters.outbound.persistence.catalog.track_row import TrackRow
from altune.domain.catalog.playlist import Playlist, PlaylistTrack
from altune.domain.catalog.playlist_id import PlaylistId
from altune.domain.catalog.track import Track
from altune.domain.catalog.track_id import TrackId
from altune.domain.shared.user_id import UserId

if TYPE_CHECKING:
    from sqlalchemy.ext.asyncio import AsyncSession


class SqlAlchemyPlaylistRepository:
    def __init__(self, session: AsyncSession) -> None:
        self._session = session

    async def create(self, playlist: Playlist) -> Playlist:
        row = PlaylistRow.from_domain(playlist)
        self._session.add(row)
        await self._session.flush()
        return row.to_domain()

    async def list_for_user(self, user_id: UserId) -> Sequence[Playlist]:
        stmt = (
            select(PlaylistRow)
            .where(PlaylistRow.user_id == user_id.value)
            .order_by(PlaylistRow.updated_at.desc())
        )
        result = await self._session.execute(stmt)
        rows = result.scalars().all()
        return [row.to_domain() for row in rows]

    async def get_track_count(self, playlist_id: PlaylistId) -> int:
        stmt = (
            select(func.count())
            .select_from(PlaylistTrackRow)
            .where(PlaylistTrackRow.playlist_id == playlist_id.value)
        )
        result = await self._session.execute(stmt)
        return result.scalar() or 0

    async def get_by_id(self, playlist_id: PlaylistId, user_id: UserId) -> Playlist | None:
        stmt = select(PlaylistRow).where(
            PlaylistRow.id == playlist_id.value,
            PlaylistRow.user_id == user_id.value,
        )
        result = await self._session.execute(stmt)
        row = result.scalar_one_or_none()
        if row is None:
            return None
        return row.to_domain()

    async def get_with_tracks(
        self,
        playlist_id: PlaylistId,
        user_id: UserId,
    ) -> tuple[Playlist, Sequence[Track]] | None:
        playlist_row = await self._get_row(playlist_id, user_id)
        if playlist_row is None:
            return None

        stmt = (
            select(PlaylistTrackRow, TrackRow)
            .join(TrackRow, PlaylistTrackRow.track_id == TrackRow.id)
            .where(PlaylistTrackRow.playlist_id == playlist_id.value)
            .order_by(PlaylistTrackRow.position)
        )
        result = await self._session.execute(stmt)
        pairs = result.all()

        playlist_tracks = tuple(
            PlaylistTrack(track_id=TrackId(pt.track_id), position=pt.position) for pt, _ in pairs
        )
        tracks = [tr.to_domain() for _, tr in pairs]
        playlist = playlist_row.to_domain(playlist_tracks)
        return playlist, tracks

    async def update_name(
        self,
        playlist_id: PlaylistId,
        user_id: UserId,
        name: str,
    ) -> Playlist | None:
        row = await self._get_row(playlist_id, user_id)
        if row is None:
            return None
        row.name = name
        row.updated_at = datetime.now(UTC)
        await self._session.flush()
        return row.to_domain()

    async def delete(self, playlist_id: PlaylistId, user_id: UserId) -> bool:
        stmt = delete(PlaylistRow).where(
            PlaylistRow.id == playlist_id.value, PlaylistRow.user_id == user_id.value
        )
        result = await self._session.execute(stmt)
        row_count: int = getattr(result, "rowcount", 0) or 0
        return row_count > 0

    async def add_track(
        self,
        playlist_id: PlaylistId,
        user_id: UserId,
        track_id: TrackId,
    ) -> bool:
        row = await self._get_row(playlist_id, user_id)
        if row is None:
            return False

        existing = await self._session.execute(
            select(PlaylistTrackRow).where(
                PlaylistTrackRow.playlist_id == playlist_id.value,
                PlaylistTrackRow.track_id == track_id.value,
            )
        )
        if existing.scalar_one_or_none() is not None:
            return False

        max_pos_result = await self._session.execute(
            select(func.coalesce(func.max(PlaylistTrackRow.position), -1)).where(
                PlaylistTrackRow.playlist_id == playlist_id.value
            )
        )
        max_pos = max_pos_result.scalar() or -1

        self._session.add(
            PlaylistTrackRow(
                playlist_id=playlist_id.value,
                track_id=track_id.value,
                position=max_pos + 1,
            )
        )
        row.updated_at = datetime.now(UTC)
        await self._session.flush()
        return True

    async def remove_track(
        self,
        playlist_id: PlaylistId,
        user_id: UserId,
        track_id: TrackId,
    ) -> bool:
        row = await self._get_row(playlist_id, user_id)
        if row is None:
            return False

        pt_result = await self._session.execute(
            select(PlaylistTrackRow).where(
                PlaylistTrackRow.playlist_id == playlist_id.value,
                PlaylistTrackRow.track_id == track_id.value,
            )
        )
        pt = pt_result.scalar_one_or_none()
        if pt is None:
            return False

        removed_pos = pt.position
        await self._session.delete(pt)

        await self._session.execute(
            sa.update(PlaylistTrackRow)
            .where(
                PlaylistTrackRow.playlist_id == playlist_id.value,
                PlaylistTrackRow.position > removed_pos,
            )
            .values(position=PlaylistTrackRow.position - 1)
        )
        row.updated_at = datetime.now(UTC)
        await self._session.flush()
        return True

    async def reorder_tracks(
        self,
        playlist_id: PlaylistId,
        user_id: UserId,
        track_ids: Sequence[TrackId],
    ) -> bool:
        row = await self._get_row(playlist_id, user_id)
        if row is None:
            return False

        for i, tid in enumerate(track_ids):
            await self._session.execute(
                sa.update(PlaylistTrackRow)
                .where(
                    PlaylistTrackRow.playlist_id == playlist_id.value,
                    PlaylistTrackRow.track_id == tid.value,
                )
                .values(position=i)
            )
        row.updated_at = datetime.now(UTC)
        await self._session.flush()
        return True

    async def get_preview_artwork(
        self,
        playlist_id: PlaylistId,
        user_id: UserId,
    ) -> Sequence[str]:
        row = await self._get_row(playlist_id, user_id)
        if row is None:
            return []

        stmt = (
            select(TrackRow.artwork_url)
            .join(PlaylistTrackRow, PlaylistTrackRow.track_id == TrackRow.id)
            .where(
                PlaylistTrackRow.playlist_id == playlist_id.value,
                TrackRow.artwork_url.is_not(None),
            )
            .order_by(PlaylistTrackRow.position)
        )
        result = await self._session.execute(stmt)
        seen: set[str] = set()
        urls: list[str] = []
        for (url,) in result.all():
            if url is not None and url not in seen and len(urls) < 4:
                seen.add(url)
                urls.append(url)
        return urls

    async def _get_row(self, playlist_id: PlaylistId, user_id: UserId) -> PlaylistRow | None:
        stmt = select(PlaylistRow).where(
            PlaylistRow.id == playlist_id.value,
            PlaylistRow.user_id == user_id.value,
        )
        result = await self._session.execute(stmt)
        return result.scalar_one_or_none()

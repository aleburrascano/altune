"""SQLAlchemy models for playlists + playlist_tracks tables."""

from __future__ import annotations

from datetime import datetime  # noqa: TC003
from uuid import UUID  # noqa: TC003

from sqlalchemy import TIMESTAMP, ForeignKey, Integer, Text
from sqlalchemy.dialects.postgresql import UUID as PgUUID  # noqa: N811
from sqlalchemy.orm import Mapped, mapped_column

from altune.adapters.outbound.persistence.base import Base
from altune.domain.catalog.playlist import Playlist, PlaylistTrack
from altune.domain.catalog.playlist_id import PlaylistId
from altune.domain.shared.user_id import UserId


class PlaylistRow(Base):
    __tablename__ = "playlists"

    id: Mapped[UUID] = mapped_column(PgUUID(as_uuid=True), primary_key=True)
    user_id: Mapped[UUID] = mapped_column(PgUUID(as_uuid=True), nullable=False)
    name: Mapped[str] = mapped_column(Text, nullable=False)
    created_at: Mapped[datetime] = mapped_column(TIMESTAMP(timezone=True), nullable=False)
    updated_at: Mapped[datetime] = mapped_column(TIMESTAMP(timezone=True), nullable=False)

    def to_domain(self, tracks: tuple[PlaylistTrack, ...] = ()) -> Playlist:
        return Playlist(
            id=PlaylistId(self.id),
            user_id=UserId(self.user_id),
            name=self.name,
            created_at=self.created_at,
            updated_at=self.updated_at,
            tracks=tracks,
        )

    @classmethod
    def from_domain(cls, p: Playlist) -> PlaylistRow:
        return cls(
            id=p.id.value,
            user_id=p.user_id.value,
            name=p.name,
            created_at=p.created_at,
            updated_at=p.updated_at,
        )


class PlaylistTrackRow(Base):
    __tablename__ = "playlist_tracks"

    playlist_id: Mapped[UUID] = mapped_column(
        PgUUID(as_uuid=True),
        ForeignKey("playlists.id", ondelete="CASCADE"),
        primary_key=True,
    )
    track_id: Mapped[UUID] = mapped_column(
        PgUUID(as_uuid=True),
        ForeignKey("tracks.id", ondelete="CASCADE"),
        primary_key=True,
    )
    position: Mapped[int] = mapped_column(Integer, nullable=False)

"""TrackRow — SQLAlchemy mapping for the `tracks` table.

The adapter owns the row<->domain conversion. The domain never sees TrackRow;
the application layer never imports SQLAlchemy. This is the seam.
"""

from __future__ import annotations

# AIDEV-NOTE: SQLAlchemy 2.0 Mapped[T] annotations are resolved at runtime
# (not just by mypy), so the names used inside Mapped[] — UUID, datetime —
# must be importable at module scope, NOT hidden under TYPE_CHECKING.
from datetime import datetime  # noqa: TC003  # see AIDEV-NOTE above
from uuid import UUID  # noqa: TC003  # see AIDEV-NOTE above

from sqlalchemy import TIMESTAMP, Integer, Text
from sqlalchemy.dialects.postgresql import (
    UUID as PgUUID,  # noqa: N811  # sqlalchemy uses UUID; we alias to avoid clash with stdlib uuid.UUID
)
from sqlalchemy.orm import Mapped, mapped_column

from altune.adapters.outbound.persistence.base import Base
from altune.domain.catalog.track import Track
from altune.domain.catalog.track_id import TrackId
from altune.domain.shared.user_id import UserId


class TrackRow(Base):
    __tablename__ = "tracks"

    id: Mapped[UUID] = mapped_column(PgUUID(as_uuid=True), primary_key=True)
    user_id: Mapped[UUID] = mapped_column(PgUUID(as_uuid=True), nullable=False)
    title: Mapped[str] = mapped_column(Text, nullable=False)
    artist: Mapped[str] = mapped_column(Text, nullable=False)
    album: Mapped[str | None] = mapped_column(Text, nullable=True)
    duration_seconds: Mapped[int | None] = mapped_column(Integer, nullable=True)
    added_at: Mapped[datetime] = mapped_column(TIMESTAMP(timezone=True), nullable=False)

    def to_domain(self) -> Track:
        return Track(
            id=TrackId(self.id),
            user_id=UserId(self.user_id),
            title=self.title,
            artist=self.artist,
            album=self.album,
            duration_seconds=self.duration_seconds,
            added_at=self.added_at,
        )

    @classmethod
    def from_domain(cls, track: Track) -> TrackRow:
        return cls(
            id=track.id.value,
            user_id=track.user_id.value,
            title=track.title,
            artist=track.artist,
            album=track.album,
            duration_seconds=track.duration_seconds,
            added_at=track.added_at,
        )

"""TrackRow — SQLAlchemy mapping for the `tracks` table.

The adapter owns the row<->domain conversion. The domain never sees TrackRow;
the application layer never imports SQLAlchemy. This is the seam.
"""

from __future__ import annotations

# AIDEV-NOTE: SQLAlchemy 2.0 Mapped[T] annotations are resolved at runtime
# (not just by mypy), so the names used inside Mapped[] — UUID, datetime —
# must be importable at module scope, NOT hidden under TYPE_CHECKING.
from datetime import datetime  # noqa: TC003  # see AIDEV-NOTE above
from typing import Any
from uuid import UUID  # noqa: TC003  # see AIDEV-NOTE above

from sqlalchemy import TIMESTAMP, Integer, Text
from sqlalchemy.dialects.postgresql import (
    UUID as PgUUID,  # noqa: N811  # sqlalchemy uses UUID; we alias to avoid clash with stdlib uuid.UUID
)
from sqlalchemy.orm import Mapped, mapped_column

from altune.adapters.outbound.persistence.base import Base
from altune.domain.catalog.acquisition_status import AcquisitionStatus
from altune.domain.catalog.dedup import dedup_key
from altune.domain.catalog.track import Track
from altune.domain.catalog.track_id import TrackId
from altune.domain.shared.user_id import UserId


def _dedup_key_default(context: Any) -> str:
    # AIDEV-NOTE: column-level default so ANY insert that omits dedup_key
    # (e.g. raw TrackRow construction in tests) still satisfies the NOT NULL
    # column and the UNIQUE(user_id, dedup_key) idempotency constraint.
    # from_domain() sets it explicitly; this is the safety net. `context` is a
    # SQLAlchemy DefaultExecutionContext — typed Any to avoid importing the
    # internal interface.
    params = context.get_current_parameters()
    return dedup_key(params["title"], params["artist"], params.get("album"))


class TrackRow(Base):
    __tablename__ = "tracks"

    id: Mapped[UUID] = mapped_column(PgUUID(as_uuid=True), primary_key=True)
    user_id: Mapped[UUID] = mapped_column(PgUUID(as_uuid=True), nullable=False)
    title: Mapped[str] = mapped_column(Text, nullable=False)
    artist: Mapped[str] = mapped_column(Text, nullable=False)
    album: Mapped[str | None] = mapped_column(Text, nullable=True)
    duration_seconds: Mapped[int | None] = mapped_column(Integer, nullable=True)
    added_at: Mapped[datetime] = mapped_column(TIMESTAMP(timezone=True), nullable=False)
    artwork_url: Mapped[str | None] = mapped_column(Text, nullable=True)
    acquisition_status: Mapped[str] = mapped_column(Text, nullable=False, server_default="pending")
    # AIDEV-NOTE: dedup_key is persistence-only — the natural key behind the
    # UNIQUE(user_id, dedup_key) idempotency constraint. It is NOT a domain
    # field; it is derived from title/artist/album via the domain normalizer.
    dedup_key: Mapped[str] = mapped_column(Text, nullable=False, default=_dedup_key_default)
    year: Mapped[int | None] = mapped_column(Integer, nullable=True)
    genre: Mapped[str | None] = mapped_column(Text, nullable=True)
    track_number: Mapped[int | None] = mapped_column(Integer, nullable=True)
    album_artist: Mapped[str | None] = mapped_column(Text, nullable=True)
    isrc: Mapped[str | None] = mapped_column(Text, nullable=True)
    audio_ref: Mapped[str | None] = mapped_column(Text, nullable=True)
    failure_reason: Mapped[str | None] = mapped_column(Text, nullable=True)

    def to_domain(self) -> Track:
        return Track(
            id=TrackId(self.id),
            user_id=UserId(self.user_id),
            title=self.title,
            artist=self.artist,
            album=self.album,
            duration_seconds=self.duration_seconds,
            added_at=self.added_at,
            artwork_url=self.artwork_url,
            acquisition_status=AcquisitionStatus(self.acquisition_status),
            year=self.year,
            genre=self.genre,
            track_number=self.track_number,
            album_artist=self.album_artist,
            isrc=self.isrc,
            audio_ref=self.audio_ref,
            failure_reason=self.failure_reason,
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
            artwork_url=track.artwork_url,
            acquisition_status=track.acquisition_status.value,
            dedup_key=dedup_key(track.title, track.artist, track.album),
            year=track.year,
            genre=track.genre,
            track_number=track.track_number,
            album_artist=track.album_artist,
            isrc=track.isrc,
            audio_ref=track.audio_ref,
            failure_reason=track.failure_reason,
        )

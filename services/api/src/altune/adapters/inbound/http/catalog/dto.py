"""Pydantic DTOs for the catalog HTTP routes.

Frozen response models per .claude/rules/python-backend.md. Wire-format
serialization happens here; the application layer never sees these types.
"""

from __future__ import annotations

from datetime import datetime  # noqa: TC003  # pydantic resolves at runtime
from uuid import UUID  # noqa: TC003  # pydantic resolves at runtime

from pydantic import BaseModel, ConfigDict


class CreateTrackRequest(BaseModel):
    title: str
    artist: str
    album: str | None = None
    duration_seconds: int | None = None
    artwork_url: str | None = None


class TrackResponse(BaseModel):
    model_config = ConfigDict(frozen=True)

    id: UUID
    title: str
    artist: str
    album: str | None
    duration_seconds: int | None
    added_at: datetime
    acquisition_status: str
    artwork_url: str | None
    year: int | None = None
    genre: str | None = None
    track_number: int | None = None
    album_artist: str | None = None
    isrc: str | None = None
    audio_ref: str | None = None


class ListTracksResponse(BaseModel):
    model_config = ConfigDict(frozen=True)

    items: list[TrackResponse]
    total: int
    limit: int
    offset: int
    has_more: bool

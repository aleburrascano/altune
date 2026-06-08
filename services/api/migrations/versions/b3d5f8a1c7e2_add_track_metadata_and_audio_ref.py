"""add track metadata columns and audio_ref

Extends the `tracks` table for the import-legacy-library spec:
- year (nullable integer) — release year
- genre (nullable text) — primary genre
- track_number (nullable integer) — position on album
- album_artist (nullable text) — album-level artist
- isrc (nullable text) — International Standard Recording Code
- audio_ref (nullable text) — opaque storage key for the audio file

All columns are nullable with no server defaults — existing rows are unaffected.

Revision ID: b3d5f8a1c7e2
Revises: a1c4e7b9d2f3
"""

from collections.abc import Sequence

import sqlalchemy as sa
from alembic import op

revision: str = "b3d5f8a1c7e2"
down_revision: str | Sequence[str] | None = "a1c4e7b9d2f3"
branch_labels: str | Sequence[str] | None = None
depends_on: str | Sequence[str] | None = None


def upgrade() -> None:
    op.add_column("tracks", sa.Column("year", sa.Integer(), nullable=True))
    op.add_column("tracks", sa.Column("genre", sa.Text(), nullable=True))
    op.add_column("tracks", sa.Column("track_number", sa.Integer(), nullable=True))
    op.add_column("tracks", sa.Column("album_artist", sa.Text(), nullable=True))
    op.add_column("tracks", sa.Column("isrc", sa.Text(), nullable=True))
    op.add_column("tracks", sa.Column("audio_ref", sa.Text(), nullable=True))


def downgrade() -> None:
    op.drop_column("tracks", "audio_ref")
    op.drop_column("tracks", "isrc")
    op.drop_column("tracks", "album_artist")
    op.drop_column("tracks", "track_number")
    op.drop_column("tracks", "genre")
    op.drop_column("tracks", "year")

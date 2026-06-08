"""add playlists and playlist_tracks tables

Creates the two tables for playlist CRUD (playlists-v1 spec):
- playlists: user-owned named collections
- playlist_tracks: ordered join table (playlist_id, track_id, position)

Revision ID: c4e6f9a2b1d8
Revises: b3d5f8a1c7e2
"""

from collections.abc import Sequence

import sqlalchemy as sa
from alembic import op

revision: str = "c4e6f9a2b1d8"
down_revision: str | Sequence[str] | None = "b3d5f8a1c7e2"
branch_labels: str | Sequence[str] | None = None
depends_on: str | Sequence[str] | None = None


def upgrade() -> None:
    op.create_table(
        "playlists",
        sa.Column("id", sa.dialects.postgresql.UUID(as_uuid=True), primary_key=True),
        sa.Column("user_id", sa.dialects.postgresql.UUID(as_uuid=True), nullable=False),
        sa.Column("name", sa.Text(), nullable=False),
        sa.Column(
            "created_at",
            sa.TIMESTAMP(timezone=True),
            nullable=False,
            server_default=sa.text("now()"),
        ),
        sa.Column(
            "updated_at",
            sa.TIMESTAMP(timezone=True),
            nullable=False,
            server_default=sa.text("now()"),
        ),
    )
    op.create_index(
        "playlists_user_updated_idx", "playlists", ["user_id", sa.text("updated_at DESC")]
    )

    op.create_table(
        "playlist_tracks",
        sa.Column("playlist_id", sa.dialects.postgresql.UUID(as_uuid=True), nullable=False),
        sa.Column("track_id", sa.dialects.postgresql.UUID(as_uuid=True), nullable=False),
        sa.Column("position", sa.Integer(), nullable=False),
        sa.PrimaryKeyConstraint("playlist_id", "track_id"),
        sa.ForeignKeyConstraint(["playlist_id"], ["playlists.id"], ondelete="CASCADE"),
        sa.ForeignKeyConstraint(["track_id"], ["tracks.id"], ondelete="CASCADE"),
    )
    op.create_index("playlist_tracks_order_idx", "playlist_tracks", ["playlist_id", "position"])


def downgrade() -> None:
    op.drop_index("playlist_tracks_order_idx", table_name="playlist_tracks")
    op.drop_table("playlist_tracks")
    op.drop_index("playlists_user_updated_idx", table_name="playlists")
    op.drop_table("playlists")

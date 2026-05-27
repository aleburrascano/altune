"""create tracks table

First business migration of altune. Revises from the walking-skeleton no-op
marker. Implements the schema declared in docs/specs/view-library/spec.md:
tenant-scoped tracks table with id-tiebreaker index supporting the
``(added_at DESC, id DESC)`` order required by the spec's AC#1.

Revision ID: f6d247fb8b49
Revises: 08b831424865
Create Date: 2026-05-27 11:05:43.331227
"""

from collections.abc import Sequence

import sqlalchemy as sa
from alembic import op
from sqlalchemy.dialects import postgresql

# revision identifiers, used by Alembic.
revision: str = "f6d247fb8b49"
down_revision: str | Sequence[str] | None = "08b831424865"
branch_labels: str | Sequence[str] | None = None
depends_on: str | Sequence[str] | None = None


def upgrade() -> None:
    """Create the tracks table + the (user_id, added_at DESC, id DESC) index."""
    op.create_table(
        "tracks",
        sa.Column(
            "id",
            postgresql.UUID(as_uuid=True),
            primary_key=True,
            server_default=sa.text("gen_random_uuid()"),
        ),
        sa.Column("user_id", postgresql.UUID(as_uuid=True), nullable=False),
        sa.Column("title", sa.Text(), nullable=False),
        sa.Column("artist", sa.Text(), nullable=False),
        sa.Column("album", sa.Text(), nullable=True),
        sa.Column("duration_seconds", sa.Integer(), nullable=True),
        sa.Column(
            "added_at",
            sa.TIMESTAMP(timezone=True),
            nullable=False,
            server_default=sa.text("now()"),
        ),
    )
    # The trailing id DESC is the stable tiebreaker per the spec's AC#1.
    op.create_index(
        "tracks_user_added_idx",
        "tracks",
        ["user_id", sa.text("added_at DESC"), sa.text("id DESC")],
    )


def downgrade() -> None:
    """Drop the tracks table and its index."""
    op.drop_index("tracks_user_added_idx", table_name="tracks")
    op.drop_table("tracks")

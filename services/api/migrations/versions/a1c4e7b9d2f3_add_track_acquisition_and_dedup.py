"""add track acquisition status, artwork_url, and dedup key

Adds the view-result-detail write-path columns to `tracks`:
- artwork_url (nullable) — cover art captured at save time
- acquisition_status (NOT NULL, default 'pending') — audio-acquisition lifecycle
- dedup_key (NOT NULL) — natural key for save idempotency, app-computed

The UNIQUE(user_id, dedup_key) constraint is the idempotency backstop (spec
AC#7). The `tracks` table is empty pre-launch, so the NOT NULL columns are
added with a temporary server_default to satisfy any existing rows, then the
dedup_key default is dropped so the application must supply it.

Revision ID: a1c4e7b9d2f3
Revises: e2bcd72a93f1
"""

from __future__ import annotations

from collections.abc import Sequence

import sqlalchemy as sa

from alembic import op

revision: str = "a1c4e7b9d2f3"
down_revision: str | None = "e2bcd72a93f1"
branch_labels: str | Sequence[str] | None = None
depends_on: str | Sequence[str] | None = None


def upgrade() -> None:
    op.add_column("tracks", sa.Column("artwork_url", sa.Text(), nullable=True))
    op.add_column(
        "tracks",
        sa.Column("acquisition_status", sa.Text(), nullable=False, server_default="pending"),
    )
    op.add_column(
        "tracks",
        sa.Column("dedup_key", sa.Text(), nullable=False, server_default=""),
    )
    op.alter_column("tracks", "dedup_key", server_default=None)
    op.create_unique_constraint("uq_tracks_user_dedup", "tracks", ["user_id", "dedup_key"])


def downgrade() -> None:
    op.drop_constraint("uq_tracks_user_dedup", "tracks", type_="unique")
    op.drop_column("tracks", "dedup_key")
    op.drop_column("tracks", "acquisition_status")
    op.drop_column("tracks", "artwork_url")

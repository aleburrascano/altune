"""add discovery_search_history table

Slice 2 of docs/specs/discover-music-v1/plan.md. Per-user search-history
ring buffer (ring-buffer logic enforced application-side at Slice 37's
SqlAlchemySearchHistoryRepository; AC#11, 12, 13, 14 of the spec). The
index supports the AC#13 distinct-recent-by-query_norm read path.

Revision ID: d15c001f8eaa
Revises: f6d247fb8b49
Create Date: 2026-05-27 23:55:00.000000
"""

# mypy: warn_unused_ignores = False
from collections.abc import Sequence

import sqlalchemy as sa  # type: ignore[import-not-found]
from alembic import op  # type: ignore[import-not-found]
from sqlalchemy.dialects import postgresql  # type: ignore[import-not-found]

# revision identifiers, used by Alembic.
revision: str = "d15c001f8eaa"
down_revision: str | Sequence[str] | None = "f6d247fb8b49"
branch_labels: str | Sequence[str] | None = None
depends_on: str | Sequence[str] | None = None


def upgrade() -> None:
    """Create the discovery_search_history table + the user/executed_at index."""
    op.create_table(
        "discovery_search_history",
        sa.Column(
            "id",
            postgresql.UUID(as_uuid=True),
            primary_key=True,
            server_default=sa.text("gen_random_uuid()"),
        ),
        sa.Column("user_id", postgresql.UUID(as_uuid=True), nullable=False),
        sa.Column("query", sa.Text(), nullable=False),
        sa.Column("query_norm", sa.Text(), nullable=False),
        sa.Column(
            "executed_at",
            sa.TIMESTAMP(timezone=True),
            nullable=False,
            server_default=sa.text("now()"),
        ),
        sa.Column("result_clicked_signature", sa.Text(), nullable=True),
    )
    op.create_index(
        "discovery_search_history_user_idx",
        "discovery_search_history",
        ["user_id", sa.text("executed_at DESC"), sa.text("id DESC")],
    )


def downgrade() -> None:
    """Drop the discovery_search_history table and its index."""
    op.drop_index("discovery_search_history_user_idx", table_name="discovery_search_history")
    op.drop_table("discovery_search_history")

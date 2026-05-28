# mypy: warn_unused_ignores = False
"""add discovery_search_clicks table

Slice 3 of docs/specs/discover-music-v1/plan.md. Per-user click-tracking
table for the AC#15 endpoint + AC#16 sliding-window idempotency. The
confidence CHECK constraint enforces the {high, medium, low} enum at the
storage boundary (defense-in-depth alongside Pydantic / domain enum).
Per ADR-0007, no UNIQUE constraint on (user_id, query_norm,
result_signature, clicked_at) — idempotency lives application-side at
Slice 40's repository.

Revision ID: e2bcd72a93f1
Revises: d15c001f8eaa
Create Date: 2026-05-27 23:56:00.000000
"""

from collections.abc import Sequence

import sqlalchemy as sa  # type: ignore[import-not-found]
from alembic import op  # type: ignore[import-not-found]
from sqlalchemy.dialects import postgresql  # type: ignore[import-not-found]

# revision identifiers, used by Alembic.
revision: str = "e2bcd72a93f1"
down_revision: str | Sequence[str] | None = "d15c001f8eaa"
branch_labels: str | Sequence[str] | None = None
depends_on: str | Sequence[str] | None = None


def upgrade() -> None:
    """Create discovery_search_clicks with a confidence CHECK constraint."""
    op.create_table(
        "discovery_search_clicks",
        sa.Column(
            "id",
            postgresql.UUID(as_uuid=True),
            primary_key=True,
            server_default=sa.text("gen_random_uuid()"),
        ),
        sa.Column("user_id", postgresql.UUID(as_uuid=True), nullable=False),
        sa.Column("query_norm", sa.Text(), nullable=False),
        sa.Column("result_signature", sa.Text(), nullable=False),
        sa.Column("position", sa.Integer(), nullable=False),
        sa.Column("confidence", sa.Text(), nullable=False),
        sa.Column(
            "clicked_at",
            sa.TIMESTAMP(timezone=True),
            nullable=False,
            server_default=sa.text("now()"),
        ),
        sa.CheckConstraint(
            "confidence IN ('high', 'medium', 'low')",
            name="discovery_search_clicks_confidence_check",
        ),
    )
    op.create_index(
        "discovery_search_clicks_dedup_idx",
        "discovery_search_clicks",
        ["user_id", "query_norm", "result_signature", sa.text("clicked_at DESC")],
    )


def downgrade() -> None:
    """Drop the discovery_search_clicks table and its index."""
    op.drop_index("discovery_search_clicks_dedup_idx", table_name="discovery_search_clicks")
    op.drop_table("discovery_search_clicks")

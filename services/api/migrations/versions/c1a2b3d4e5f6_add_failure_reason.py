"""add failure_reason column to tracks

Supports the acquire-track spec AC#8: failed acquisitions store a
human-readable reason on the Track aggregate.

Revision ID: c1a2b3d4e5f6
Revises: e2bcd72a93f1
"""

from collections.abc import Sequence

import sqlalchemy as sa
from alembic import op

revision: str = "c1a2b3d4e5f6"
down_revision: str | Sequence[str] | None = "e2bcd72a93f1"
branch_labels: str | Sequence[str] | None = None
depends_on: str | Sequence[str] | None = None


def upgrade() -> None:
    op.add_column("tracks", sa.Column("failure_reason", sa.Text(), nullable=True))


def downgrade() -> None:
    op.drop_column("tracks", "failure_reason")

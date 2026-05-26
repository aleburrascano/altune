"""initial marker — walking-skeleton no-op per ADR-0003.

Confirms the Alembic toolchain runs end-to-end against a real Postgres
before any business tables exist. The first business migration (likely
`create_tracks_table` from the view-library feature) revises from this one.

Revision ID: 08b831424865
Revises:
Create Date: 2026-05-26 19:45:10.041941
"""

from collections.abc import Sequence

# revision identifiers, used by Alembic.
revision: str = "08b831424865"
down_revision: str | Sequence[str] | None = None
branch_labels: str | Sequence[str] | None = None
depends_on: str | Sequence[str] | None = None


def upgrade() -> None:
    """No-op marker — see module docstring."""


def downgrade() -> None:
    """No-op marker — see module docstring."""

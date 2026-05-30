"""add track acquisition status, artwork_url, and dedup key

Adds the view-result-detail write-path columns to `tracks`:
- artwork_url (nullable) — cover art captured at save time
- acquisition_status (NOT NULL, default 'pending') — audio-acquisition lifecycle
- dedup_key (NOT NULL) — natural key for save idempotency, app-computed

The UNIQUE(user_id, dedup_key) constraint is the idempotency backstop (spec
AC#7). dedup_key is added with a temporary '' server_default to satisfy NOT
NULL on existing rows, then **backfilled** from each row's title/artist/album
using the same normalization as the domain `dedup_key()` so the unique index
builds on a non-empty table (e.g. a dev DB with seeded tracks), not only on the
empty pre-launch table. The temporary default is then dropped so the
application must supply the key on every insert.

Revision ID: a1c4e7b9d2f3
Revises: e2bcd72a93f1
"""

from collections.abc import Sequence

import sqlalchemy as sa
from alembic import op

revision: str = "a1c4e7b9d2f3"
down_revision: str | Sequence[str] | None = "e2bcd72a93f1"
branch_labels: str | Sequence[str] | None = None
depends_on: str | Sequence[str] | None = None

# AIDEV-NOTE: inline copy of the domain dedup_key normalizer
# (altune.domain.catalog.dedup). Migrations must be self-contained snapshots and
# must not import app code that can change underneath an old revision, so the
# normalization is duplicated here. Keep in sync if the domain rule ever changes
# (it would need a new migration to re-key existing rows anyway).
_SEP = "\x1f"


def _norm(value: str) -> str:
    return " ".join(value.split()).casefold()


def _dedup_key(title: str, artist: str, album: str | None) -> str:
    return _SEP.join((_norm(title), _norm(artist), _norm(album or "")))


def _backfill_dedup_key() -> None:
    bind = op.get_bind()
    rows = bind.execute(sa.text("SELECT id, title, artist, album FROM tracks")).fetchall()
    for row in rows:
        bind.execute(
            sa.text("UPDATE tracks SET dedup_key = :key WHERE id = :id"),
            {"key": _dedup_key(row.title, row.artist, row.album), "id": row.id},
        )


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
    _backfill_dedup_key()
    op.alter_column("tracks", "dedup_key", server_default=None)
    op.create_unique_constraint("uq_tracks_user_dedup", "tracks", ["user_id", "dedup_key"])


def downgrade() -> None:
    op.drop_constraint("uq_tracks_user_dedup", "tracks", type_="unique")
    op.drop_column("tracks", "dedup_key")
    op.drop_column("tracks", "acquisition_status")
    op.drop_column("tracks", "artwork_url")

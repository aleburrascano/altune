"""SearchHistoryRow — SQLAlchemy mapping for discovery_search_history."""

from __future__ import annotations

# AIDEV-NOTE: SQLAlchemy 2.0 Mapped[T] annotations resolved at runtime;
# UUID + datetime must be importable at module scope.
from datetime import datetime  # noqa: TC003
from uuid import UUID  # noqa: TC003

from sqlalchemy import TIMESTAMP, Text
from sqlalchemy.dialects.postgresql import UUID as PgUUID  # noqa: N811
from sqlalchemy.orm import Mapped, mapped_column

from altune.adapters.outbound.persistence.base import Base
from altune.domain.discovery.search_history_entry import (
    SearchHistoryEntry,
    SearchHistoryEntryId,
)
from altune.domain.shared.user_id import UserId


class SearchHistoryRow(Base):
    __tablename__ = "discovery_search_history"

    id: Mapped[UUID] = mapped_column(PgUUID(as_uuid=True), primary_key=True)
    user_id: Mapped[UUID] = mapped_column(PgUUID(as_uuid=True), nullable=False)
    query: Mapped[str] = mapped_column(Text, nullable=False)
    query_norm: Mapped[str] = mapped_column(Text, nullable=False)
    executed_at: Mapped[datetime] = mapped_column(TIMESTAMP(timezone=True), nullable=False)
    result_clicked_signature: Mapped[str | None] = mapped_column(Text, nullable=True)

    def to_domain(self) -> SearchHistoryEntry:
        return SearchHistoryEntry(
            id=SearchHistoryEntryId(self.id),
            user_id=UserId(self.user_id),
            query=self.query,
            query_norm=self.query_norm,
            executed_at=self.executed_at,
            result_clicked_signature=self.result_clicked_signature,
        )

    @classmethod
    def from_domain(cls, entry: SearchHistoryEntry) -> SearchHistoryRow:
        return cls(
            id=entry.id.value,
            user_id=entry.user_id.value,
            query=entry.query,
            query_norm=entry.query_norm,
            executed_at=entry.executed_at,
            result_clicked_signature=entry.result_clicked_signature,
        )

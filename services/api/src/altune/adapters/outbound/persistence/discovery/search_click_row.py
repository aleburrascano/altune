"""SearchClickRow — SQLAlchemy mapping for discovery_search_clicks."""

from __future__ import annotations

from datetime import datetime  # noqa: TC003
from uuid import UUID  # noqa: TC003

from sqlalchemy import TIMESTAMP, Integer, Text
from sqlalchemy.dialects.postgresql import UUID as PgUUID  # noqa: N811
from sqlalchemy.orm import Mapped, mapped_column

from altune.adapters.outbound.persistence.base import Base
from altune.domain.discovery.confidence import Confidence
from altune.domain.discovery.search_click import SearchClick, SearchClickId
from altune.domain.shared.user_id import UserId


class SearchClickRow(Base):
    __tablename__ = "discovery_search_clicks"

    id: Mapped[UUID] = mapped_column(PgUUID(as_uuid=True), primary_key=True)
    user_id: Mapped[UUID] = mapped_column(PgUUID(as_uuid=True), nullable=False)
    query_norm: Mapped[str] = mapped_column(Text, nullable=False)
    result_signature: Mapped[str] = mapped_column(Text, nullable=False)
    position: Mapped[int] = mapped_column(Integer, nullable=False)
    confidence: Mapped[str] = mapped_column(Text, nullable=False)
    clicked_at: Mapped[datetime] = mapped_column(TIMESTAMP(timezone=True), nullable=False)

    def to_domain(self) -> SearchClick:
        return SearchClick(
            id=SearchClickId(self.id),
            user_id=UserId(self.user_id),
            query_norm=self.query_norm,
            result_signature=self.result_signature,
            position=self.position,
            confidence=Confidence(self.confidence),
            clicked_at=self.clicked_at,
        )

    @classmethod
    def from_domain(cls, click: SearchClick) -> SearchClickRow:
        return cls(
            id=click.id.value,
            user_id=click.user_id.value,
            query_norm=click.query_norm,
            result_signature=click.result_signature,
            position=click.position,
            confidence=click.confidence.value,
            clicked_at=click.clicked_at,
        )

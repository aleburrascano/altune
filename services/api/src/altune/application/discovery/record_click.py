"""RecordClick — slice 41 use case for POST /v1/discovery/clicks.

result_signature = sha256(f"{kind}|{normalize_for_match(title)}|
{normalize_for_match(subtitle or '')}")[:12]. Per spec definition; lets
clicks on the same canonical result via different sources collapse to
one signature.
"""

from __future__ import annotations

import hashlib
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import TYPE_CHECKING
from uuid import uuid4

from altune.application.discovery.normalize import normalize_for_match
from altune.domain.discovery.search_click import SearchClick, SearchClickId

if TYPE_CHECKING:
    from altune.application.discovery.ports import (
        ClickInsertOutcome,
        SearchClickRepository,
    )
    from altune.domain.discovery.confidence import Confidence
    from altune.domain.discovery.result_kind import ResultKind
    from altune.domain.shared.user_id import UserId


@dataclass(frozen=True, slots=True)
class RecordClickInput:
    user_id: UserId
    query_norm: str
    kind: ResultKind
    title: str
    subtitle: str | None
    position: int
    confidence: Confidence


@dataclass(frozen=True, slots=True)
class RecordClickOutput:
    outcome: ClickInsertOutcome
    result_signature: str


def compute_result_signature(kind: ResultKind, title: str, subtitle: str | None) -> str:
    """Deterministic result_signature per spec definition."""
    norm_title = normalize_for_match(title)
    norm_subtitle = normalize_for_match(subtitle or "")
    raw = f"{kind.value}|{norm_title}|{norm_subtitle}"
    return hashlib.sha256(raw.encode("utf-8")).hexdigest()[:12]


@dataclass
class RecordClick:
    click_repo: SearchClickRepository
    window_seconds: int = 60

    async def execute(self, request: RecordClickInput) -> RecordClickOutput:
        signature = compute_result_signature(request.kind, request.title, request.subtitle)
        click = SearchClick(
            id=SearchClickId(uuid4()),
            user_id=request.user_id,
            query_norm=request.query_norm,
            result_signature=signature,
            position=request.position,
            confidence=request.confidence,
            clicked_at=datetime.now(UTC),
        )
        outcome = await self.click_repo.insert_if_outside_window(
            click, self.window_seconds
        )
        return RecordClickOutput(outcome=outcome, result_signature=signature)

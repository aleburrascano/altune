"""ListTracks use case — read-side of the catalog bounded context.

Returns one page of the current user's tracks plus the total count and a
``has_more`` indicator derived from ``(offset + len(items)) < total``.

Per the spec, ``total`` is a per-request snapshot — concurrent writes can
shift it between paged calls. v1 has no writers so this is academic.

Emits a ``tracks_listed`` structlog event on success per the spec's
Telemetry section. The HTTP inbound adapter emits ``http_get_tracks_request``
separately so the inbound and use-case sides are independently traceable
(the hardcoded-user_id scenario from ADR-0004 is the most likely failure
mode and demands independent observability).
"""

from __future__ import annotations

from dataclasses import dataclass
from typing import TYPE_CHECKING

import structlog

if TYPE_CHECKING:
    from altune.application.catalog.ports import TrackRepository
    from altune.domain.catalog.track import Track
    from altune.domain.shared.user_id import UserId

log = structlog.get_logger(__name__)


@dataclass(frozen=True, slots=True)
class ListTracksInput:
    user_id: UserId
    limit: int
    offset: int


@dataclass(frozen=True, slots=True)
class ListTracksOutput:
    items: tuple[Track, ...]
    total: int
    limit: int
    offset: int
    has_more: bool


class ListTracks:
    def __init__(self, tracks: TrackRepository) -> None:
        self._tracks = tracks

    async def execute(self, input: ListTracksInput) -> ListTracksOutput:
        items, total = await self._tracks.list_for_user(input.user_id, input.limit, input.offset)
        has_more = (input.offset + len(items)) < total
        log.info(
            "tracks_listed",
            user_id=str(input.user_id),
            limit=input.limit,
            offset=input.offset,
            returned_count=len(items),
            total=total,
        )
        return ListTracksOutput(
            items=tuple(items),
            total=total,
            limit=input.limit,
            offset=input.offset,
            has_more=has_more,
        )

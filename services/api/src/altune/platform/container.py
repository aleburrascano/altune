"""Dependency injection container.

Wires ports (defined in `application/<context>/ports.py`) to concrete adapter implementations
(defined in `adapters/outbound/<kind>/<context>/`).

For now this is a stub; the first feature that needs DI will populate it. The shape is:

    @lru_cache
    def get_track_repository() -> TrackRepository:
        return SqlAlchemyTrackRepository(session=...)

    # In FastAPI router:
    @router.post("/tracks")
    async def register(
        body: RegisterTrackBody,
        tracks: TrackRepository = Depends(get_track_repository),
    ): ...
"""

from __future__ import annotations

# AIDEV-NOTE: per-context providers added here as features land.
# Keep this file thin — it's wiring only, no business logic.

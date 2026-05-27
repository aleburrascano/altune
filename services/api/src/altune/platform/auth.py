"""current_user_id — the single identity port into the application layer.

Per ADR-0004. In v1 this resolves to the value of ``settings.hardcoded_user_id``;
when real auth lands (its own future ADR) only the body of this function
swaps — every use case keeps its ``user_id: UserId`` signature, every column
keeps its NOT NULL, every test keeps its shape.

The settings instance is pulled from ``app.state.settings`` (stored by the
lifespan in ``platform/app.py``) so a FastAPI app can be constructed with a
custom Settings for tests without monkey-patching.
"""

from __future__ import annotations

# AIDEV-NOTE: FastAPI introspects parameter annotations at runtime to wire
# Request injection — Request CANNOT live under TYPE_CHECKING here or
# FastAPI treats `request: "Request"` as an unresolved query param and
# returns 422 "Field required" instead of injecting the framework Request.
from fastapi import Request  # noqa: TC002  # see AIDEV-NOTE above

from altune.domain.shared.user_id import UserId


def current_user_id(request: Request) -> UserId:
    settings = request.app.state.settings
    if settings.hardcoded_user_id is None:
        # AIDEV-WARNING: this is dev/test mode per ADR-0004. The prod-startup
        # guard in Settings refuses to construct in env=production with this
        # field set, so reaching here in production means env is non-prod or
        # the guard was bypassed — either way, configuration is wrong.
        raise RuntimeError(
            "HARDCODED_USER_ID is unset; cannot resolve current_user_id "
            "(see ADR-0004)."
        )
    return UserId(settings.hardcoded_user_id)

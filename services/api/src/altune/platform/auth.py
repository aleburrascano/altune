# mypy: warn_unused_ignores = False
"""current_user_id — the single identity port into the application layer.

Per ADR-0006 (auth-integration spec) — supersedes ADR-0004's hardcoded-user
posture. This dependency extracts the Authorization header, strips the
`Bearer ` prefix (case-insensitive), and delegates to the TokenVerifier
instance on `app.state.token_verifier` (constructed in the lifespan from
Settings). On any failure it raises `InvalidTokenError`; Slice 5's HTTP
error mapper translates that to a 401 response.

The signature is unchanged from ADR-0004 — every use case keeps its
``user_id: UserId`` input and every WHERE clause keeps its parameter. Only
the body's resolution path changed: hardcoded id → verified JWT sub claim.
"""

from __future__ import annotations

from typing import TYPE_CHECKING, cast

# AIDEV-NOTE: FastAPI introspects parameter annotations at runtime to wire
# Request injection — Request CANNOT live under TYPE_CHECKING here or
# FastAPI treats `request: "Request"` as an unresolved query param and
# returns 422 "Field required" instead of injecting the framework Request.
from fastapi import Request  # type: ignore[import-not-found]  # noqa: TC002

from altune.application.auth.exceptions import (  # type: ignore[import-not-found]
    InvalidTokenError,
    TokenRejectReason,
)

if TYPE_CHECKING:
    from altune.application.auth.ports import TokenVerifier
    from altune.domain.shared.user_id import UserId

_BEARER_PREFIX = "bearer "


async def current_user_id(request: Request) -> UserId:
    """Resolve the caller's UserId from the Authorization header's JWT.

    Raises InvalidTokenError if the header is missing/malformed or if the
    bearer fails verification. Slice 5's exception handler maps that to 401.
    """
    raw_authorization = request.headers.get("authorization")
    if not raw_authorization:
        raise InvalidTokenError(TokenRejectReason.MISSING)

    # Case-insensitive "Bearer " prefix; whitespace-tolerant after the scheme.
    if not raw_authorization.lower().startswith(_BEARER_PREFIX):
        raise InvalidTokenError(TokenRejectReason.MALFORMED)
    bearer = raw_authorization[len(_BEARER_PREFIX) :].strip()
    if not bearer:
        raise InvalidTokenError(TokenRejectReason.MALFORMED)

    verifier = cast("TokenVerifier", request.app.state.token_verifier)
    return await verifier.verify(bearer)

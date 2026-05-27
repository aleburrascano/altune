"""TokenVerifier port — the application's contract for resolving a bearer to a UserId.

Per ADR-0006 (auth-integration spec): a single port at the application layer
hides the concrete verifier (`SupabaseJwtVerifier` in `adapters/outbound/auth/`)
from the inbound HTTP layer. The FastAPI `current_user_id` dependency consumes
this port; the test stub (`tests/_doubles/InMemoryTokenVerifier`) implements it.

Verification raises `InvalidTokenError` on any failure; success returns a
`UserId`. The verifier owns deciding what counts as failure (signature,
expiry, claim mismatch, etc.) — the port is intentionally narrow.
"""

from __future__ import annotations

from typing import TYPE_CHECKING, Protocol

if TYPE_CHECKING:
    from altune.domain.shared.user_id import UserId


class TokenVerifier(Protocol):
    """Exchanges a raw bearer string for a verified UserId.

    Implementations:
    - `altune.adapters.outbound.auth.supabase_jwt_verifier.SupabaseJwtVerifier`
    - `tests._doubles.in_memory_token_verifier.InMemoryTokenVerifier`
    """

    async def verify(self, raw_bearer: str) -> UserId:
        """Verify the bearer and return the caller's UserId.

        Raises:
            InvalidTokenError: with a populated `reason` if verification fails.
                The bearer's claims are NOT propagated to the caller on failure.
        """
        ...

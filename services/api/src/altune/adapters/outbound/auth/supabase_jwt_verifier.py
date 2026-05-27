# mypy: warn_unused_ignores = False
"""SupabaseJwtVerifier — outbound adapter implementing the TokenVerifier port.

Per ADR-0006: JWKS-mode verification (RS256/ES256), `pyjwt[crypto]` as the
underlying library, 30s symmetric leeway on `exp` / `nbf`. The verifier is
constructed once at app startup (lifespan in `platform/app.py`) and stored
on `app.state.token_verifier`; the `current_user_id` FastAPI dependency
consumes it (Slice 4 wires the swap).

Slice 3a's scope: happy-path verification + signature-mismatch + non-UUID
`sub` rejection. Claim validation (`iss`, `aud`, `exp` beyond leeway) lands
in Slice 3b. JWKS cache + refresh-on-kid-miss telemetry lands in Slice 3c.
Through 3a the verifier loads its JWKS once at construction via the
injected `jwks_provider` callable; tests inject a fixture-built dict, and
the production wiring injects a closure that fetches from the Supabase URL.
"""

from __future__ import annotations

from typing import TYPE_CHECKING, Any
from uuid import UUID

import jwt  # type: ignore[import-not-found]
from jwt.algorithms import RSAAlgorithm  # type: ignore[import-not-found]

from altune.application.auth.exceptions import (  # type: ignore[import-not-found]
    InvalidTokenError,
    TokenRejectReason,
)
from altune.domain.shared.user_id import UserId  # type: ignore[import-not-found]

if TYPE_CHECKING:
    from collections.abc import Callable

_LEEWAY_SECONDS = 30


class SupabaseJwtVerifier:
    """Verifies a Supabase-issued JWT and yields a UserId on success.

    Constructed once at app startup with the project's iss/aud expectations
    and a JWKS provider (callable returning a JWKS dict). Slice 3c replaces
    the one-shot load with a cache that refreshes on unknown `kid`.
    """

    def __init__(
        self,
        iss_expected: str,
        aud_expected: str,
        jwks_provider: Callable[[], dict[str, Any]],
    ) -> None:
        self._iss = iss_expected
        self._aud = aud_expected
        self._jwks_provider = jwks_provider
        # AIDEV-NOTE: Slice 3a loads JWKS once at construction. Slice 3c
        # replaces this with a kid-keyed cache + refresh-on-miss.
        self._jwks: dict[str, Any] = jwks_provider()

    async def verify(self, raw_bearer: str) -> UserId:
        # Find the key matching the token's kid (Slice 3a: single-key happy path).
        try:
            unverified_header = jwt.get_unverified_header(raw_bearer)
        except jwt.exceptions.DecodeError as exc:
            raise InvalidTokenError(TokenRejectReason.MALFORMED) from exc

        kid = unverified_header.get("kid")
        key = self._find_key(kid)

        try:
            payload = jwt.decode(
                raw_bearer,
                key=key,
                algorithms=["RS256", "ES256"],
                options={
                    # Slice 3b adds iss/aud claim validation; Slice 3a only
                    # validates signature + sub. exp is enforced because pyjwt
                    # checks it by default; leeway is 30s symmetric.
                    "verify_aud": False,
                    "verify_iss": False,
                    "require": ["sub"],
                },
                leeway=_LEEWAY_SECONDS,
            )
        except jwt.exceptions.InvalidSignatureError as exc:
            raise InvalidTokenError(TokenRejectReason.SIGNATURE_INVALID) from exc
        except jwt.exceptions.ExpiredSignatureError as exc:
            raise InvalidTokenError(TokenRejectReason.EXPIRED) from exc
        except jwt.exceptions.MissingRequiredClaimError as exc:
            raise InvalidTokenError(TokenRejectReason.CLAIM_INVALID_SUB) from exc
        except jwt.exceptions.InvalidTokenError as exc:
            raise InvalidTokenError(TokenRejectReason.MALFORMED) from exc

        sub_raw = payload.get("sub")
        if not isinstance(sub_raw, str) or not sub_raw:
            raise InvalidTokenError(TokenRejectReason.CLAIM_INVALID_SUB)
        try:
            sub_uuid = UUID(sub_raw)
        except (ValueError, TypeError) as exc:
            raise InvalidTokenError(TokenRejectReason.CLAIM_INVALID_SUB) from exc

        return UserId(sub_uuid)

    def _find_key(self, kid: str | None) -> Any:
        for jwk in self._jwks.get("keys", []):
            if kid is None or jwk.get("kid") == kid:
                return RSAAlgorithm.from_jwk(jwk)
        # No matching key — Slice 3c refreshes here; Slice 3a treats this as
        # signature failure (the verifier can't trust an unknown kid).
        raise InvalidTokenError(TokenRejectReason.SIGNATURE_INVALID)

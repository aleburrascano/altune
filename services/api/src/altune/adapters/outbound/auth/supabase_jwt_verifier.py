# mypy: warn_unused_ignores = False
"""SupabaseJwtVerifier — outbound adapter implementing the TokenVerifier port.

Per ADR-0006: JWKS-mode verification (RS256/ES256), `pyjwt[crypto]` as the
underlying library, 30s symmetric leeway on `exp` / `nbf`. The verifier is
constructed once at app startup (lifespan in `platform/app.py`) and stored
on `app.state.token_verifier`; the `current_user_id` FastAPI dependency
consumes it (Slice 4 wires the swap).

JWKS handling (Slice 3c): a kid-keyed cache populated at construction. On a
`verify()` call whose token's `kid` is not in the cache, the verifier calls
`jwks_provider()` again and replaces the cache. Each refresh emits an
`auth.jwks_refreshed` structlog event with the kids added/removed and the
cache age in seconds. Successful verifications within the cache do NOT
trigger a refresh (no TTL-driven refetch in v1; only refresh-on-miss).
"""

from __future__ import annotations

import time
from typing import TYPE_CHECKING, Any
from uuid import UUID

import jwt  # type: ignore[import-not-found]
import structlog  # type: ignore[import-not-found]
from jwt.algorithms import ECAlgorithm, RSAAlgorithm  # type: ignore[import-not-found]

from altune.application.auth.exceptions import (  # type: ignore[import-not-found]
    InvalidTokenError,
    TokenRejectReason,
)
from altune.domain.shared.user_id import UserId  # type: ignore[import-not-found]

if TYPE_CHECKING:
    from collections.abc import Callable

_LEEWAY_SECONDS = 30

log = structlog.get_logger(__name__)


class SupabaseJwtVerifier:
    """Verifies a Supabase-issued JWT and yields a UserId on success.

    Constructed once at app startup with the project's iss/aud expectations
    and a JWKS provider (callable returning a JWKS dict). The provider is
    called on construction (populating the initial cache) and again on every
    `kid` cache miss.
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
        self._jwks_by_kid: dict[str, Any] = {}
        self._loaded_at: float = 0.0
        self._refresh_cache()

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
                audience=self._aud,
                issuer=self._iss,
                options={"require": ["sub", "iss", "aud", "exp"]},
                leeway=_LEEWAY_SECONDS,
            )
        except jwt.exceptions.InvalidSignatureError as exc:
            raise InvalidTokenError(TokenRejectReason.SIGNATURE_INVALID) from exc
        except jwt.exceptions.ExpiredSignatureError as exc:
            raise InvalidTokenError(TokenRejectReason.EXPIRED) from exc
        except jwt.exceptions.InvalidIssuerError as exc:
            raise InvalidTokenError(TokenRejectReason.CLAIM_INVALID_ISS) from exc
        except jwt.exceptions.InvalidAudienceError as exc:
            raise InvalidTokenError(TokenRejectReason.CLAIM_INVALID_AUD) from exc
        except jwt.exceptions.MissingRequiredClaimError as exc:
            # Map sub-specific to CLAIM_INVALID_SUB; iss/aud to their own;
            # exp missing falls through to MALFORMED.
            claim = getattr(exc, "claim", "")
            if claim == "sub":
                raise InvalidTokenError(TokenRejectReason.CLAIM_INVALID_SUB) from exc
            if claim == "iss":
                raise InvalidTokenError(TokenRejectReason.CLAIM_INVALID_ISS) from exc
            if claim == "aud":
                raise InvalidTokenError(TokenRejectReason.CLAIM_INVALID_AUD) from exc
            raise InvalidTokenError(TokenRejectReason.MALFORMED) from exc
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
        if kid is None:
            raise InvalidTokenError(TokenRejectReason.SIGNATURE_INVALID)
        if kid in self._jwks_by_kid:
            return self._jwk_to_key(self._jwks_by_kid[kid])
        # Cache miss — refresh once and look again. If still missing, the
        # token's kid is unknown to the trusted JWKS — reject.
        self._refresh_cache()
        if kid in self._jwks_by_kid:
            return self._jwk_to_key(self._jwks_by_kid[kid])
        raise InvalidTokenError(TokenRejectReason.SIGNATURE_INVALID)

    @staticmethod
    def _jwk_to_key(jwk: dict[str, Any]) -> Any:
        # AIDEV-NOTE: dispatch on kty so ES256 (Supabase's new default per
        # 2024+ rotation) works alongside legacy RS256 projects. ADR-0006
        # called for both; the original implementation hard-coded RSA.
        kty = jwk.get("kty")
        if kty == "RSA":
            return RSAAlgorithm.from_jwk(jwk)
        if kty == "EC":
            return ECAlgorithm.from_jwk(jwk)
        raise InvalidTokenError(TokenRejectReason.SIGNATURE_INVALID)

    def _refresh_cache(self) -> None:
        old_kids = set(self._jwks_by_kid.keys())
        new_jwks = self._jwks_provider()
        new_by_kid: dict[str, Any] = {
            jwk["kid"]: jwk for jwk in new_jwks.get("keys", []) if "kid" in jwk
        }
        now = time.time()
        cache_age = int(now - self._loaded_at) if self._loaded_at else 0
        self._jwks_by_kid = new_by_kid
        self._loaded_at = now
        new_kids = set(new_by_kid.keys())
        log.info(
            "auth.jwks_refreshed",
            kids_added=sorted(new_kids - old_kids),
            kids_removed=sorted(old_kids - new_kids),
            cache_age_seconds=cache_age,
        )

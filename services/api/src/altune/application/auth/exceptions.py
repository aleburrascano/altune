"""Auth-layer exceptions.

Auth is a cross-cutting concern, not a bounded context, so these exceptions
live in `application/auth/` rather than `domain/auth/`. The inbound HTTP error
mapper translates `InvalidTokenError` to HTTP 401 (Slice 5).

Per ADR-0006 (auth-integration spec): `reason` is the load-bearing classifier
for telemetry — the `auth.token_rejected` log event emits it. The string
values match the telemetry contract verbatim.
"""

from __future__ import annotations

from enum import StrEnum


class TokenRejectReason(StrEnum):
    """Why a bearer token was rejected. Values match the telemetry contract."""

    MISSING = "missing"
    MALFORMED = "malformed"
    SIGNATURE_INVALID = "signature_invalid"
    EXPIRED = "expired"
    CLAIM_INVALID_ISS = "claim_invalid_iss"
    CLAIM_INVALID_AUD = "claim_invalid_aud"
    CLAIM_INVALID_SUB = "claim_invalid_sub"


class InvalidTokenError(Exception):
    """Raised by a `TokenVerifier` when a bearer cannot be exchanged for a `UserId`.

    The reason is mandatory and surfaces in the `auth.token_rejected` log event.
    No claim values are carried — the caller is untrusted; we do not propagate
    its claims past the rejection boundary.
    """

    def __init__(self, reason: TokenRejectReason, detail: str | None = None) -> None:
        self.reason = reason
        super().__init__(detail or reason.value)

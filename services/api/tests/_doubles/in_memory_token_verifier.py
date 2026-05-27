# mypy: warn_unused_ignores = False
"""InMemoryTokenVerifier — a stub TokenVerifier for unit tests.

Per [vault: wiki/concepts/Test Double.md], this is a Fowler-style **stub**:
returns canned responses based on input, no interaction verification. Two
modes:

1. Mapping mode: `mapping={bearer: UserId}` returns the configured UserId for
   known bearers; raises `InvalidTokenError(SIGNATURE_INVALID)` for unknown.
2. Always-fail mode: `raise_reason=...` raises `InvalidTokenError` on every
   `verify()` call, regardless of input. Used to exercise rejection paths in
   consumer tests (e.g., the `current_user_id` dependency in Slice 4).

The two modes are mutually exclusive — construction enforces this. Callers
that need a fine-grained failure-per-bearer stub can compose two instances.
"""

from __future__ import annotations

from typing import TYPE_CHECKING

from altune.application.auth.exceptions import (  # type: ignore[import-not-found]
    InvalidTokenError,
    TokenRejectReason,
)

if TYPE_CHECKING:
    from altune.domain.shared.user_id import UserId  # type: ignore[import-not-found]


class InMemoryTokenVerifier:
    def __init__(
        self,
        mapping: dict[str, UserId] | None = None,
        raise_reason: TokenRejectReason | None = None,
    ) -> None:
        if mapping is not None and raise_reason is not None:
            raise ValueError(
                "InMemoryTokenVerifier: mapping and raise_reason are mutually exclusive"
            )
        self._mapping: dict[str, UserId] = mapping if mapping is not None else {}
        self._raise_reason = raise_reason

    async def verify(self, raw_bearer: str) -> UserId:
        if self._raise_reason is not None:
            raise InvalidTokenError(self._raise_reason)
        if raw_bearer in self._mapping:
            return self._mapping[raw_bearer]
        raise InvalidTokenError(TokenRejectReason.SIGNATURE_INVALID)

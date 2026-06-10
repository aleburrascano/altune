"""SpyMbidResolver — canned-answer MbidResolver that records calls.

Spy per the Meszaros taxonomy: returns a pre-programmed mbid and lets the
test assert post-hoc which provider URLs were looked up (or that none were).
"""

from __future__ import annotations

from dataclasses import dataclass, field


@dataclass
class SpyMbidResolver:
    """MbidResolver double: returns ``canned`` and records every call."""

    canned: str | None = None
    calls: list[str] = field(default_factory=list)

    async def resolve(self, provider_url: str) -> str | None:
        self.calls.append(provider_url)
        return self.canned

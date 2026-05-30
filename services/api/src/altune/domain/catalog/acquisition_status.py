"""AcquisitionStatus — value object for a saved track's audio-acquisition state.

A Track enters the library carrying metadata only; its audio is acquired later
(yt-dlp -> OCI) by the future `acquire-track` spec. This enum tracks that
lifecycle. v1 ships the single starting state; later specs add
`acquiring` / `ready` / `failed`.

Wire-serialized as a lowercase string, matching the discovery enums.
"""

from __future__ import annotations

from enum import Enum


class AcquisitionStatus(Enum):
    """Lifecycle of a saved track's audio acquisition."""

    PENDING = "pending"  # saved to library; audio not yet acquired

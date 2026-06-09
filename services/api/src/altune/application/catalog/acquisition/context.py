"""AcquisitionContext — mutable state shared across pipeline steps."""

from __future__ import annotations

from dataclasses import dataclass, field
from pathlib import Path
from typing import TYPE_CHECKING

if TYPE_CHECKING:
    from altune.application.catalog.ports import AudioCandidate
    from altune.domain.catalog.track import Track


@dataclass
class AcquisitionContext:
    track: Track | None = None
    candidates: list[AudioCandidate] = field(default_factory=list)
    selected: AudioCandidate | None = None
    temp_path: Path | None = None
    audio_ref: str | None = None

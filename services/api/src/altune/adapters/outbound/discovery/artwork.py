"""Chained cover-art resolution across providers.

Tries each resolver in order and returns the first usable cover URL. Deezer
is broad and fast but sometimes returns its empty-artist placeholder (the
md5-of-empty-string image); we skip that and fall through to TheAudioDB,
which has higher-quality art for the artists it covers.
"""

from __future__ import annotations

import logging
from dataclasses import dataclass
from typing import TYPE_CHECKING

if TYPE_CHECKING:
    from collections.abc import Sequence

    from altune.application.discovery.ports import ArtworkResolver
    from altune.domain.discovery.result_kind import ResultKind

_log = logging.getLogger(__name__)

# Deezer serves this image for artists/albums it has no real artwork for — it's
# the md5 of the empty string. Treat it as "no art" so we fall through.
_DEEZER_EMPTY_IMAGE_HASH = "d41d8cd98f00b204e9800998ecf8427e"


def _is_usable(url: str | None) -> bool:
    return bool(url) and _DEEZER_EMPTY_IMAGE_HASH not in (url or "")


@dataclass
class ChainedArtworkResolver:
    """An ArtworkResolver that delegates to others in order (first usable wins)."""

    resolvers: Sequence[ArtworkResolver]

    async def resolve_artwork(
        self,
        kind: ResultKind,
        title: str,
        subtitle: str | None,
    ) -> str | None:
        for resolver in self.resolvers:
            try:
                url = await resolver.resolve_artwork(kind, title, subtitle)
            except Exception:
                _log.warning("artwork_resolver_failed", exc_info=True)
                url = None
            if _is_usable(url):
                return url
        return None

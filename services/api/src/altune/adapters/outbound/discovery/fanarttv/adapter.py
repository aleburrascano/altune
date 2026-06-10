# mypy: warn_unused_ignores = False
"""Fanart.tv artwork resolver — MBID-based artist image lookup.

Fanart.tv provides high-quality artist images (thumbnails, backgrounds,
logos) indexed by MusicBrainz ID. Unlike TheAudioDB (name-based search),
this never returns the wrong artist for common names.

API docs: https://fanarttv.docs.apiary.io/
"""

from __future__ import annotations

import logging
from dataclasses import dataclass
from typing import TYPE_CHECKING

from altune.domain.discovery.result_kind import ResultKind

if TYPE_CHECKING:
    import httpx

_log = logging.getLogger(__name__)
_BASE_URL = "https://webservice.fanart.tv/v3/music"


@dataclass
class FanartTvArtworkResolver:
    """ArtworkResolver that looks up artist images by MBID on Fanart.tv."""

    client: httpx.AsyncClient
    api_key: str
    base_url: str = _BASE_URL

    async def resolve_artwork(
        self,
        kind: ResultKind,
        title: str,
        subtitle: str | None,
        *,
        mbid: str | None = None,
    ) -> str | None:
        if mbid is None:
            return None
        try:
            response = await self.client.get(
                f"{self.base_url}/{mbid}",
                params={"api_key": self.api_key},
            )
            if response.status_code == 404:
                return None
            response.raise_for_status()
            data = response.json()

            if kind is ResultKind.ARTIST:
                thumbs = data.get("artistthumb", [])
                if thumbs:
                    return str(thumbs[0].get("url", ""))
                bgs = data.get("artistbackground", [])
                if bgs:
                    return str(bgs[0].get("url", ""))

            covers = data.get("albumcover", [])
            if covers:
                return str(covers[0].get("url", ""))

            return None
        except Exception:
            _log.warning("fanarttv_artwork_failed mbid=%s", mbid, exc_info=True)
            return None

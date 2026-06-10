# mypy: warn_unused_ignores = False
"""Genius artwork resolver — artist image lookup by name.

Genius has strong hip-hop/underground coverage. Used as a fallback after
Fanart.tv (MBID-based) for artists Fanart.tv doesn't cover. Searches by
artist name and returns the header_image_url from the first matching artist.

API docs: https://docs.genius.com/
"""

from __future__ import annotations

import logging
from dataclasses import dataclass
from typing import TYPE_CHECKING

from altune.domain.discovery.result_kind import ResultKind

if TYPE_CHECKING:
    import httpx

_log = logging.getLogger(__name__)
_BASE_URL = "https://api.genius.com"


@dataclass
class GeniusArtworkResolver:
    """ArtworkResolver that searches Genius for artist images."""

    client: httpx.AsyncClient
    access_token: str
    base_url: str = _BASE_URL

    async def resolve_artwork(
        self,
        kind: ResultKind,
        title: str,
        subtitle: str | None,
    ) -> str | None:
        if kind is ResultKind.ARTIST:
            return await self._resolve_artist_image(title)
        if subtitle:
            return await self._resolve_song_image(title, subtitle)
        return None

    async def _resolve_song_image(self, title: str, artist: str) -> str | None:
        """Search for a specific song and return its cover image."""
        try:
            response = await self.client.get(
                f"{self.base_url}/search",
                params={"q": f"{artist} {title}"},
                headers={"Authorization": f"Bearer {self.access_token}"},
            )
            response.raise_for_status()
            data = response.json()
            hits = data.get("response", {}).get("hits", [])
            for hit in hits:
                result = hit.get("result", {})
                img = result.get("song_art_image_url") or result.get("header_image_url")
                if img and "default" not in img and "no_image" not in img:
                    return str(img)
            return None
        except Exception:
            _log.warning("genius_song_artwork_failed title=%s", title, exc_info=True)
            return None

    async def _resolve_artist_image(self, artist_name: str) -> str | None:
        artist_name = artist_name.strip()
        try:
            response = await self.client.get(
                f"{self.base_url}/search",
                params={"q": artist_name},
                headers={"Authorization": f"Bearer {self.access_token}"},
            )
            response.raise_for_status()
            data = response.json()
            hits = data.get("response", {}).get("hits", [])
            for hit in hits:
                result = hit.get("result", {})
                artist = result.get("primary_artist", {})
                name = (artist.get("name") or "").strip()
                if name.lower() == artist_name.lower():
                    img = artist.get("image_url")
                    if img and "default" not in img and "no_image" not in img:
                        return str(img)
            # Fallback: search "<artist> songs" to find songs BY this artist
            response2 = await self.client.get(
                f"{self.base_url}/search",
                params={"q": f"{artist_name} songs"},
                headers={"Authorization": f"Bearer {self.access_token}"},
            )
            response2.raise_for_status()
            data2 = response2.json()
            hits2 = data2.get("response", {}).get("hits", [])
            for hit in hits2:
                result = hit.get("result", {})
                artist = result.get("primary_artist", {})
                name = (artist.get("name") or "").strip()
                if name.lower() == artist_name.lower():
                    img = artist.get("image_url")
                    if img and "default" not in img and "no_image" not in img:
                        return str(img)
            return None
        except Exception:
            _log.warning("genius_artwork_failed artist=%s", artist_name, exc_info=True)
            return None

# mypy: warn_unused_ignores = False
"""Wikidata SPARQL adapter — cross-provider ID bridge + Wikimedia images.

Resolves Deezer artist IDs to MBIDs (and vice versa) via Wikidata's
linked-data properties. Also retrieves Wikimedia Commons artist images.

Properties used:
  P434 — MusicBrainz artist ID
  P2722 — Deezer artist ID
  P18 — image (Wikimedia Commons)
"""

from __future__ import annotations

import logging
from dataclasses import dataclass
from typing import TYPE_CHECKING

if TYPE_CHECKING:
    import httpx

_log = logging.getLogger(__name__)
_SPARQL_URL = "https://query.wikidata.org/sparql"


@dataclass
class WikidataMbidResolver:
    """Resolve a Deezer artist ID to an MBID via Wikidata SPARQL."""

    client: httpx.AsyncClient

    async def resolve_from_deezer_id(self, deezer_artist_id: str) -> str | None:
        query = (
            "SELECT ?mbid WHERE { "
            f'?item wdt:P2722 "{deezer_artist_id}" ; '
            "wdt:P434 ?mbid . "
            "} LIMIT 1"
        )
        return await self._run_sparql(query, "mbid")

    async def resolve_image_from_mbid(self, mbid: str) -> str | None:
        query = f'SELECT ?image WHERE {{ ?item wdt:P434 "{mbid}" ; wdt:P18 ?image . }} LIMIT 1'
        return await self._run_sparql(query, "image")

    async def _run_sparql(self, query: str, field: str) -> str | None:
        try:
            response = await self.client.get(
                _SPARQL_URL,
                params={"query": query, "format": "json"},
                headers={"Accept": "application/sparql-results+json"},
            )
            if response.status_code == 429:
                return None
            response.raise_for_status()
            data = response.json()
            bindings = data.get("results", {}).get("bindings", [])
            if bindings:
                val = bindings[0].get(field, {}).get("value")
                if isinstance(val, str) and val:
                    return val
            return None
        except Exception:
            _log.warning("wikidata_sparql_failed field=%s", field, exc_info=True)
            return None

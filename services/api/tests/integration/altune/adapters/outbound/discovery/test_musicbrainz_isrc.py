# mypy: warn_unused_ignores = False, disable_error_code = "no-any-return,untyped-decorator"
"""MusicBrainz ISRC enablement (inc=isrcs) — ranking-overhaul addendum.

Reviving the ISRC dedup signal: MB recording search now requests `inc=isrcs`
and parses the `isrcs[]` array into `extras["isrc"]`, giving Deezer/iTunes an
ISRC partner for high-precision cross-source merges.
"""

from __future__ import annotations

import httpx
import pytest
import respx

from altune.adapters.outbound.discovery.musicbrainz.adapter import MusicBrainzSearchAdapter
from altune.domain.discovery.provider_status import ProviderStatus
from altune.domain.discovery.result_kind import ResultKind


@pytest.mark.integration
@pytest.mark.asyncio
@respx.mock
async def test_musicbrainz_adapter_parses_isrc_when_present() -> None:
    payload = {
        "recordings": [
            {
                "id": "11111111-1111-1111-1111-111111111111",
                "title": "Let It Be",
                "artist-credit": [{"name": "The Beatles"}],
                "isrcs": ["GBAYE0601477", "GBAYE0601478"],
            }
        ]
    }
    respx.get("https://musicbrainz.org/ws/2/recording").mock(
        return_value=httpx.Response(200, json=payload)
    )
    async with httpx.AsyncClient() as client:
        adapter = MusicBrainzSearchAdapter(client=client)
        resp = await adapter.search("let it be", frozenset({ResultKind.TRACK}), limit=5)
    assert resp.status is ProviderStatus.OK
    assert len(resp.results) == 1
    # First ISRC of the array becomes the canonical match key.
    assert resp.results[0].extras["isrc"] == "GBAYE0601477"


@pytest.mark.integration
@pytest.mark.asyncio
@respx.mock
async def test_musicbrainz_adapter_isrc_none_when_absent() -> None:
    payload = {
        "recordings": [
            {
                "id": "22222222-2222-2222-2222-222222222222",
                "title": "Obscure Track",
                "artist-credit": [{"name": "Unknown"}],
            }
        ]
    }
    respx.get("https://musicbrainz.org/ws/2/recording").mock(
        return_value=httpx.Response(200, json=payload)
    )
    async with httpx.AsyncClient() as client:
        adapter = MusicBrainzSearchAdapter(client=client)
        resp = await adapter.search("obscure", frozenset({ResultKind.TRACK}), limit=5)
    assert resp.results[0].extras["isrc"] is None

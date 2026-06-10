# mypy: warn_unused_ignores = False, disable_error_code = "no-any-return,untyped-decorator"
"""GeniusArtworkResolver — respx-mocked integration tests (track-hint search)."""

from __future__ import annotations

import httpx
import pytest
import respx

from altune.adapters.outbound.discovery.genius.adapter import GeniusArtworkResolver
from altune.domain.discovery.result_kind import ResultKind

_BASE = "https://api.genius.com"
_CHE_IMAGE = "https://images.genius.com/che.jpg"


def _payload(*hits: tuple[str, str, str | None]) -> dict:  # type: ignore[type-arg]
    """Build a Genius search payload from (title, artist_name, image_url) hits."""
    return {
        "response": {
            "hits": [
                {
                    "result": {
                        "title": title,
                        "primary_artist": {"name": artist, "image_url": image},
                    }
                }
                for title, artist, image in hits
            ]
        }
    }


_WRONG_ARTISTS = _payload(
    ("Mo Bamba", "Sheck Wes", "https://images.genius.com/wrong1.jpg"),
    ("Cheap Thrills", "Sia", "https://images.genius.com/wrong2.jpg"),
)


@pytest.mark.integration
@pytest.mark.asyncio
@respx.mock
async def test_genius_track_hint_finds_artist_when_name_search_fails() -> None:
    respx.get(_BASE + "/search", params={"q": "Che"}).mock(
        return_value=httpx.Response(200, json=_WRONG_ARTISTS)
    )
    respx.get(_BASE + "/search", params={"q": "Che songs"}).mock(
        return_value=httpx.Response(200, json=_WRONG_ARTISTS)
    )
    respx.get(_BASE + "/search", params={"q": "Che agenda"}).mock(
        return_value=httpx.Response(200, json=_payload(("agenda", "Che", _CHE_IMAGE)))
    )
    async with httpx.AsyncClient() as client:
        resolver = GeniusArtworkResolver(client=client, access_token="t")
        url = await resolver.resolve_artwork(
            ResultKind.ARTIST, "Che", None, track_hints=("agenda",)
        )
    assert url == _CHE_IMAGE


@pytest.mark.integration
@pytest.mark.asyncio
@respx.mock
async def test_genius_artist_name_search_still_works_without_hints() -> None:
    respx.get(_BASE + "/search", params={"q": "Mac DeMarco"}).mock(
        return_value=httpx.Response(
            200,
            json=_payload(("Chamber of Reflection", "Mac DeMarco", _CHE_IMAGE)),
        )
    )
    async with httpx.AsyncClient() as client:
        resolver = GeniusArtworkResolver(client=client, access_token="t")
        url = await resolver.resolve_artwork(ResultKind.ARTIST, "Mac DeMarco", None)
    assert url == _CHE_IMAGE


@pytest.mark.integration
@pytest.mark.asyncio
@respx.mock
async def test_genius_returns_none_when_hints_exhausted() -> None:
    for q in ("Che", "Che songs", "Che agenda", "Che Bae"):
        respx.get(_BASE + "/search", params={"q": q}).mock(
            return_value=httpx.Response(200, json=_WRONG_ARTISTS)
        )
    async with httpx.AsyncClient() as client:
        resolver = GeniusArtworkResolver(client=client, access_token="t")
        url = await resolver.resolve_artwork(
            ResultKind.ARTIST, "Che", None, track_hints=("agenda", "Bae")
        )
    assert url is None


@pytest.mark.integration
@pytest.mark.asyncio
@respx.mock
async def test_genius_caps_hint_searches_at_three() -> None:
    for q in ("Che", "Che songs", "Che h1", "Che h2", "Che h3"):
        respx.get(_BASE + "/search", params={"q": q}).mock(
            return_value=httpx.Response(200, json=_WRONG_ARTISTS)
        )
    fourth = respx.get(_BASE + "/search", params={"q": "Che h4"}).mock(
        return_value=httpx.Response(200, json=_WRONG_ARTISTS)
    )
    async with httpx.AsyncClient() as client:
        resolver = GeniusArtworkResolver(client=client, access_token="t")
        url = await resolver.resolve_artwork(
            ResultKind.ARTIST, "Che", None, track_hints=("h1", "h2", "h3", "h4")
        )
    assert url is None
    assert not fourth.called

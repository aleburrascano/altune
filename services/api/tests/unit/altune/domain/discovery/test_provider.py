"""ProviderName enum + SourceRef value object — slice 5 of discover-music-v1.

Per ADR-0007 source set + AC#1 sources-array shape.
"""

from __future__ import annotations

import pytest

from altune.domain.discovery.provider import ProviderName
from altune.domain.discovery.source_ref import SourceRef


@pytest.mark.unit
def test_provider_name_has_six_members() -> None:
    # iTunes + TheAudioDB added by discover-music-v2.
    assert {m.value for m in ProviderName} == {
        "deezer",
        "musicbrainz",
        "soundcloud",
        "lastfm",
        "itunes",
        "theaudiodb",
    }


@pytest.mark.unit
def test_source_ref_is_frozen_and_compares_by_value() -> None:
    a = SourceRef(provider=ProviderName.DEEZER, external_id="3135556", url="https://deezer.com/track/3135556")
    b = SourceRef(provider=ProviderName.DEEZER, external_id="3135556", url="https://deezer.com/track/3135556")
    c = SourceRef(provider=ProviderName.MUSICBRAINZ, external_id="abc", url="https://musicbrainz.org/recording/abc")
    assert a == b
    assert a != c
    assert hash(a) == hash(b)


@pytest.mark.unit
def test_source_ref_rejects_empty_external_id() -> None:
    with pytest.raises(ValueError, match="external_id"):
        SourceRef(provider=ProviderName.DEEZER, external_id="", url="https://example.com")


@pytest.mark.unit
def test_source_ref_rejects_empty_url() -> None:
    with pytest.raises(ValueError, match="url"):
        SourceRef(provider=ProviderName.DEEZER, external_id="3135556", url="")

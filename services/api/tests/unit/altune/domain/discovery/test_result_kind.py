"""ResultKind enum — slice 4 of discover-music-v1.

Four members covering AC#1's wire shape: artist, album, track, playlist.
"""

from __future__ import annotations

import pytest
from altune.domain.discovery.result_kind import ResultKind


@pytest.mark.unit
def test_result_kind_has_exact_four_members() -> None:
    assert {m.value for m in ResultKind} == {"artist", "album", "track", "playlist"}


@pytest.mark.unit
def test_result_kind_round_trips_via_value() -> None:
    assert ResultKind("artist") is ResultKind.ARTIST
    assert ResultKind("album") is ResultKind.ALBUM
    assert ResultKind("track") is ResultKind.TRACK
    assert ResultKind("playlist") is ResultKind.PLAYLIST

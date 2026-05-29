"""ResultKind enum.

Three members covering the wire shape: artist, album, track. Playlist was
removed in discover-music-v2.
"""

from __future__ import annotations

import pytest

from altune.domain.discovery.result_kind import ResultKind


@pytest.mark.unit
def test_result_kind_has_exact_three_members() -> None:
    assert {m.value for m in ResultKind} == {"artist", "album", "track"}


@pytest.mark.unit
def test_result_kind_round_trips_via_value() -> None:
    assert ResultKind("artist") is ResultKind.ARTIST
    assert ResultKind("album") is ResultKind.ALBUM
    assert ResultKind("track") is ResultKind.TRACK

"""Unit tests for the dedup_key normalizer — the 'same track' rule.

dedup_key is the natural key behind the UNIQUE(user_id, dedup_key) constraint
that makes saving a track idempotent (spec AC#7). Both the in-memory fake and
the Postgres adapter call this one function, so they dedup identically.
"""

from __future__ import annotations

import pytest
from altune.domain.catalog.dedup import dedup_key


@pytest.mark.unit
def test_dedup_key_is_case_insensitive() -> None:
    assert dedup_key("Bohemian Rhapsody", "Queen", "A Night at the Opera") == dedup_key(
        "bohemian rhapsody", "QUEEN", "a night at the opera"
    )


@pytest.mark.unit
def test_dedup_key_trims_and_collapses_whitespace() -> None:
    assert dedup_key("  Song   Title ", " The  Band ", None) == dedup_key(
        "Song Title", "The Band", None
    )


@pytest.mark.unit
def test_dedup_key_null_album_equals_empty_album() -> None:
    assert dedup_key("t", "a", None) == dedup_key("t", "a", "")


@pytest.mark.unit
def test_dedup_key_distinguishes_different_tracks() -> None:
    assert dedup_key("Song A", "Artist", None) != dedup_key("Song B", "Artist", None)
    assert dedup_key("Song", "Artist", "Album One") != dedup_key("Song", "Artist", "Album Two")

"""normalize_for_match() — slice 11 of discover-music-v1.

8-step rule list from spec §3.6. Drives dedup confidence at slice 14.
"""

from __future__ import annotations

import pytest
from altune.application.discovery.normalize import normalize_for_match
from hypothesis import given
from hypothesis import strategies as st


@pytest.mark.unit
@pytest.mark.parametrize(
    ("raw", "expected"),
    [
        # NFKC + lowercase
        ("THE BEATLES", "beatles"),
        ("the beatles", "beatles"),
        # diacritics
        ("Beyoncé", "beyonce"),
        ("Mötley Crüe", "motley crue"),
        # bracketed suffixes dropped
        ("Let It Be (Remastered 2009)", "let it be"),
        ("Hey Jude [Deluxe Edition]", "hey jude"),
        ("Smells Like Teen Spirit (feat. Krist)", "smells like teen spirit"),
        # feature notation
        ("Song feat. Other", "song feat other"),
        ("Song ft. Other", "song feat other"),
        ("Song featuring Other", "song feat other"),
        # leading article on artist names
        ("The Smiths", "smiths"),
        ("Los Lobos", "lobos"),
        # 'the the' stays because the second 'the' is part of the actual name
        ("The The", "the"),
        # punctuation + whitespace collapse; & → and
        ("Salt & Pepper", "salt and pepper"),
        ("It's Me", "its me"),
        ("Trailing   spaces", "trailing spaces"),
        ("  Trim me  ", "trim me"),
    ],
)
def test_normalize_worked_examples(raw: str, expected: str) -> None:
    assert normalize_for_match(raw) == expected


@pytest.mark.unit
def test_normalize_is_idempotent() -> None:
    raw = "The Beatles - Let It Be (Remastered 2009)"
    once = normalize_for_match(raw)
    twice = normalize_for_match(once)
    assert once == twice


@pytest.mark.unit
@given(s=st.text(min_size=0, max_size=200))
def test_normalize_never_raises_on_arbitrary_text(s: str) -> None:
    # Property: normalize must accept any string without raising.
    result = normalize_for_match(s)
    assert isinstance(result, str)


@pytest.mark.unit
def test_normalize_collapses_case_diacritic_article_variants() -> None:
    # Property: "obviously same" pairs should map to the same string.
    variants = [
        "The Beatles",
        "THE BEATLES",
        "the beatles",
        "The Béatles",
    ]
    normalized = {normalize_for_match(v) for v in variants}
    assert len(normalized) == 1

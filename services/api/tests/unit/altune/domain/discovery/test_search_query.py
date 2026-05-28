"""SearchQuery + ProviderStatus — slice 7 of discover-music-v1.

Per AC#17 (validation) + AC#5/5a/5b/6 (status enum).
"""

from __future__ import annotations

import pytest
from altune.domain.discovery.provider_status import ProviderStatus
from altune.domain.discovery.search_query import SearchQuery

from altune.domain.discovery.result_kind import ResultKind


@pytest.mark.unit
def test_search_query_rejects_empty_raw() -> None:
    with pytest.raises(ValueError, match="raw"):
        SearchQuery(
            raw="",
            query_norm="beatles",
            kinds=frozenset({ResultKind.TRACK}),
            limit=25,
        )


@pytest.mark.unit
def test_search_query_rejects_limit_above_50() -> None:
    with pytest.raises(ValueError, match="limit"):
        SearchQuery(
            raw="the beatles",
            query_norm="beatles",
            kinds=frozenset({ResultKind.TRACK}),
            limit=51,
        )


@pytest.mark.unit
def test_search_query_rejects_limit_zero() -> None:
    with pytest.raises(ValueError, match="limit"):
        SearchQuery(
            raw="the beatles",
            query_norm="beatles",
            kinds=frozenset({ResultKind.TRACK}),
            limit=0,
        )


@pytest.mark.unit
def test_search_query_rejects_empty_kinds() -> None:
    with pytest.raises(ValueError, match="kinds"):
        SearchQuery(
            raw="the beatles",
            query_norm="beatles",
            kinds=frozenset(),
            limit=25,
        )


@pytest.mark.unit
def test_provider_status_has_five_members() -> None:
    assert {m.value for m in ProviderStatus} == {
        "ok",
        "timeout",
        "error",
        "rate_limited",
        "circuit_open",
    }

"""Tests for EntityResolutionTier — ordered enum (3 members: MBID > ISRC > NONE)."""

from __future__ import annotations

import pytest

from altune.domain.discovery.entity_resolution_tier import EntityResolutionTier


class TestEntityResolutionTierOrdering:
    def test_mbid_is_highest(self) -> None:
        assert EntityResolutionTier.MBID > EntityResolutionTier.ISRC
        assert EntityResolutionTier.MBID > EntityResolutionTier.NONE

    def test_isrc_above_none(self) -> None:
        assert EntityResolutionTier.ISRC > EntityResolutionTier.NONE

    def test_none_is_lowest(self) -> None:
        assert EntityResolutionTier.NONE < EntityResolutionTier.ISRC
        assert EntityResolutionTier.NONE < EntityResolutionTier.MBID

    def test_equal_tiers_are_not_greater(self) -> None:
        assert not (EntityResolutionTier.MBID > EntityResolutionTier.MBID)

    def test_ge_with_equal(self) -> None:
        assert EntityResolutionTier.MBID >= EntityResolutionTier.MBID
        assert EntityResolutionTier.MBID >= EntityResolutionTier.ISRC

    def test_comparison_with_non_tier_returns_not_implemented(self) -> None:
        assert EntityResolutionTier.MBID.__gt__("not a tier") is NotImplemented


class TestEntityResolutionTierMembers:
    def test_has_exactly_three_members(self) -> None:
        assert len(EntityResolutionTier) == 3

    def test_wire_values_are_lowercase(self) -> None:
        for tier in EntityResolutionTier:
            assert tier.value == tier.value.lower()

"""UserId constructs, hashes, compares, and rejects non-UUID values."""

from __future__ import annotations

from dataclasses import FrozenInstanceError
from uuid import UUID

import pytest

from altune.domain.shared.user_id import UserId

UUID_A = UUID("00000000-0000-0000-0000-000000000001")
UUID_B = UUID("00000000-0000-0000-0000-000000000002")


@pytest.mark.unit
def test_user_id_wraps_uuid_value() -> None:
    uid = UserId(UUID_A)
    assert uid.value == UUID_A


@pytest.mark.unit
def test_user_id_equality_by_value() -> None:
    assert UserId(UUID_A) == UserId(UUID_A)
    assert UserId(UUID_A) != UserId(UUID_B)


@pytest.mark.unit
def test_user_id_is_hashable_by_value() -> None:
    assert hash(UserId(UUID_A)) == hash(UserId(UUID_A))
    assert {UserId(UUID_A), UserId(UUID_A)} == {UserId(UUID_A)}


@pytest.mark.unit
def test_user_id_is_frozen() -> None:
    uid = UserId(UUID_A)
    with pytest.raises(FrozenInstanceError):
        uid.value = UUID_B  # type: ignore[misc]  # intentional: testing immutability


@pytest.mark.unit
def test_user_id_str_is_canonical_uuid() -> None:
    assert str(UserId(UUID_A)) == "00000000-0000-0000-0000-000000000001"


@pytest.mark.unit
def test_user_id_rejects_string_input() -> None:
    with pytest.raises(TypeError, match=r"must be a uuid\.UUID"):
        UserId("00000000-0000-0000-0000-000000000001")  # type: ignore[arg-type]  # intentional: testing rejection


@pytest.mark.unit
def test_user_id_rejects_int_input() -> None:
    with pytest.raises(TypeError, match=r"must be a uuid\.UUID"):
        UserId(42)  # type: ignore[arg-type]  # intentional: testing rejection

"""FilesystemAudioStore tests."""

from __future__ import annotations

from pathlib import Path

import pytest

from altune.adapters.outbound.audio.filesystem_store import FilesystemAudioStore


@pytest.mark.unit
async def test_filesystem_store_creates_user_artist_album_path(tmp_path: Path) -> None:
    store = FilesystemAudioStore(str(tmp_path))
    source = tmp_path / "temp.mp3"
    source.write_bytes(b"\x00" * 100)
    ref = "user-uuid/Artist/Album/Song.mp3"

    result = await store.store(source, ref)

    assert result == ref
    dest = tmp_path / ref
    assert dest.exists()
    assert dest.stat().st_size == 100
    assert not source.exists()


@pytest.mark.unit
async def test_filesystem_store_creates_nested_dirs(tmp_path: Path) -> None:
    store = FilesystemAudioStore(str(tmp_path))
    source = tmp_path / "temp.mp3"
    source.write_bytes(b"\x00" * 50)
    ref = "deep/nested/path/file.mp3"

    await store.store(source, ref)

    assert (tmp_path / ref).exists()


@pytest.mark.unit
async def test_filesystem_store_overwrites_existing(tmp_path: Path) -> None:
    store = FilesystemAudioStore(str(tmp_path))
    dest = tmp_path / "user" / "file.mp3"
    dest.parent.mkdir(parents=True)
    dest.write_bytes(b"old")

    source = tmp_path / "new.mp3"
    source.write_bytes(b"new content")

    await store.store(source, "user/file.mp3")

    assert dest.read_bytes() == b"new content"

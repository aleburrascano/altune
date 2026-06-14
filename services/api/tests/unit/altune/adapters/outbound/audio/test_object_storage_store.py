"""ObjectStorageAudioStore tests — unit tests with a stubbed S3 client."""

from __future__ import annotations

from pathlib import Path
from unittest.mock import MagicMock, patch

import pytest

from altune.adapters.outbound.audio.object_storage_store import ObjectStorageAudioStore


def _make_store() -> tuple[ObjectStorageAudioStore, MagicMock]:
    """Build a store with a mocked boto3 client."""
    mock_client = MagicMock()
    with patch("boto3.client", return_value=mock_client):
        store = ObjectStorageAudioStore(
            endpoint_url="https://ns.compat.objectstorage.eu-frankfurt-1.oraclecloud.com",
            access_key="test-key",
            secret_key="test-secret",
            bucket="altune-audio",
            region="eu-frankfurt-1",
        )
    return store, mock_client


@pytest.mark.unit
def test_exists_returns_true_when_object_present() -> None:
    store, client = _make_store()
    client.head_object.return_value = {"ContentLength": 1000}

    assert store.exists("Artist/Album/Track.mp3") is True
    client.head_object.assert_called_once_with(
        Bucket="altune-audio", Key="Artist/Album/Track.mp3"
    )


@pytest.mark.unit
def test_exists_returns_false_when_object_missing() -> None:
    store, client = _make_store()
    client.head_object.side_effect = Exception("Not Found")

    assert store.exists("missing.mp3") is False


@pytest.mark.unit
async def test_store_uploads_file_with_content_type(tmp_path: Path) -> None:
    store, client = _make_store()
    source = tmp_path / "temp.mp3"
    source.write_bytes(b"\xff\xfb\x90\x00" + b"\x00" * 100)

    ref = await store.store(source, "Artist/Album/Track.mp3")

    assert ref == "Artist/Album/Track.mp3"
    client.put_object.assert_called_once()
    call_kwargs = client.put_object.call_args[1]
    assert call_kwargs["Bucket"] == "altune-audio"
    assert call_kwargs["Key"] == "Artist/Album/Track.mp3"
    assert call_kwargs["ContentType"] == "audio/mpeg"


@pytest.mark.unit
async def test_stream_returns_chunks_for_existing_object() -> None:
    store, client = _make_store()
    body_mock = MagicMock()
    body_mock.read.return_value = b"\x00" * 200
    client.get_object.return_value = {"Body": body_mock}

    iterator = await store.stream("Artist/Album/Track.mp3")
    assert iterator is not None

    chunks = [chunk async for chunk in iterator]
    total = b"".join(chunks)
    assert len(total) == 200


@pytest.mark.unit
async def test_stream_returns_none_for_missing_object() -> None:
    store, client = _make_store()
    client.get_object.side_effect = Exception("NoSuchKey")

    result = await store.stream("missing.mp3")
    assert result is None


@pytest.mark.unit
async def test_resolve_local_path_downloads_to_temp() -> None:
    store, client = _make_store()

    def _fake_download(bucket: str, key: str, path: str) -> None:
        Path(path).write_bytes(b"\xff\xfb" + b"\x00" * 50)

    client.download_file.side_effect = _fake_download

    result = await store.resolve_local_path("Artist/Album/Track.mp3")
    assert result is not None
    assert result.exists()
    assert result.stat().st_size == 52


@pytest.mark.unit
async def test_resolve_local_path_returns_none_on_failure() -> None:
    store, client = _make_store()
    client.download_file.side_effect = Exception("Download failed")

    result = await store.resolve_local_path("missing.mp3")
    assert result is None

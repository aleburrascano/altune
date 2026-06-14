"""ObjectStorageAudioStore — S3-compatible OCI Object Storage adapter."""

from __future__ import annotations

import asyncio
import tempfile
from pathlib import Path
from typing import TYPE_CHECKING, Any

import structlog

if TYPE_CHECKING:
    from collections.abc import AsyncIterator

_logger = structlog.get_logger(__name__)

_CHUNK_SIZE = 64 * 1024  # 64 KB


class ObjectStorageAudioStore:
    """AudioStore backed by OCI Object Storage via the S3-compatible API."""

    def __init__(
        self,
        endpoint_url: str,
        access_key: str,
        secret_key: str,
        bucket: str,
        region: str,
    ) -> None:
        self._bucket = bucket
        import boto3
        from botocore.config import Config

        self._client: Any = boto3.client(
            "s3",
            endpoint_url=endpoint_url,
            aws_access_key_id=access_key,
            aws_secret_access_key=secret_key,
            region_name=region,
            config=Config(
                s3={"payload_signing_enabled": True},
                signature_version="s3v4",
                request_checksum_calculation="when_required",
                response_checksum_validation="when_required",
            ),
        )

    def exists(self, audio_ref: str) -> bool:
        try:
            self._client.head_object(Bucket=self._bucket, Key=audio_ref)
        except Exception:
            return False
        return True

    async def store(self, source_path: Path, audio_ref: str) -> str:
        def _upload() -> None:
            with open(source_path, "rb") as f:
                self._client.put_object(
                    Bucket=self._bucket,
                    Key=audio_ref,
                    Body=f,
                    ContentType="audio/mpeg",
                )

        await asyncio.to_thread(_upload)
        _logger.info(
            "audio_file_stored_object_storage",
            audio_ref=audio_ref,
            bucket=self._bucket,
            size_bytes=source_path.stat().st_size,
        )
        return audio_ref

    async def resolve_local_path(self, audio_ref: str) -> Path | None:
        tmp = Path(tempfile.mkdtemp()) / Path(audio_ref).name

        def _download() -> bool:
            try:
                self._client.download_file(self._bucket, audio_ref, str(tmp))
            except Exception:
                return False
            return True

        ok = await asyncio.to_thread(_download)
        if not ok:
            _logger.warning("object_storage_download_failed", audio_ref=audio_ref)
            return None
        _logger.info(
            "audio_fetched_object_storage",
            audio_ref=audio_ref,
            size=tmp.stat().st_size,
        )
        return tmp

    async def stream(self, audio_ref: str) -> AsyncIterator[bytes] | None:
        def _get_body() -> bytes | None:
            try:
                response = self._client.get_object(
                    Bucket=self._bucket, Key=audio_ref
                )
                return bytes(response["Body"].read())
            except Exception:
                return None

        data = await asyncio.to_thread(_get_body)
        if data is None:
            _logger.warning("object_storage_stream_not_found", audio_ref=audio_ref)
            return None

        _logger.info(
            "audio_stream_started",
            audio_ref=audio_ref,
            store_type="object_storage",
            size_bytes=len(data),
        )

        async def _gen() -> AsyncIterator[bytes]:
            offset = 0
            while offset < len(data):
                yield data[offset : offset + _CHUNK_SIZE]
                offset += _CHUNK_SIZE

        return _gen()

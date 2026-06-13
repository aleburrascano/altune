"""SshAudioStore — streams audio files from a remote server over SSH."""

from __future__ import annotations

import asyncio
import subprocess
import tempfile
from pathlib import Path

import structlog

_logger = structlog.get_logger(__name__)


class SshAudioStore:
    """AudioStore backed by a remote SSH host. Files are fetched to a temp
    directory on demand for streaming, then cleaned up by the caller."""

    def __init__(
        self,
        host: str,
        user: str,
        remote_base: str,
        key_path: str | None = None,
    ) -> None:
        self._host = host
        self._user = user
        self._remote_base = remote_base.rstrip("/")
        self._key_path = key_path

    def _ssh_cmd(self, remote_cmd: str) -> list[str]:
        cmd = ["ssh", "-o", "ConnectTimeout=10", "-o", "StrictHostKeyChecking=no"]
        if self._key_path:
            cmd += ["-i", self._key_path]
        cmd += [f"{self._user}@{self._host}", remote_cmd]
        return cmd

    def _scp_download_cmd(self, remote_path: str, local_path: str) -> list[str]:
        cmd = ["scp", "-o", "ConnectTimeout=10", "-o", "StrictHostKeyChecking=no"]
        if self._key_path:
            cmd += ["-i", self._key_path]
        cmd += [f"{self._user}@{self._host}:{remote_path}", local_path]
        return cmd

    def _scp_upload_cmd(self, local_path: str, remote_path: str) -> list[str]:
        cmd = ["scp", "-o", "ConnectTimeout=10", "-o", "StrictHostKeyChecking=no"]
        if self._key_path:
            cmd += ["-i", self._key_path]
        cmd += [local_path, f"{self._user}@{self._host}:{remote_path}"]
        return cmd

    def exists(self, audio_ref: str) -> bool:

        remote_path = f"{self._remote_base}/{audio_ref}"
        result = subprocess.run(
            self._ssh_cmd(f"test -f '{remote_path}' && echo yes || echo no"),
            capture_output=True,
            text=True,
            timeout=15,
        )
        return result.stdout.strip() == "yes"

    async def store(self, source_path: Path, audio_ref: str) -> str:
        remote_path = f"{self._remote_base}/{audio_ref}"
        remote_dir = "/".join(remote_path.rsplit("/")[:-1])

        def _do_store() -> None:
            subprocess.run(
                self._ssh_cmd(f"mkdir -p '{remote_dir}'"),
                capture_output=True,
                timeout=15,
            )
            result = subprocess.run(
                self._scp_upload_cmd(str(source_path), remote_path),
                capture_output=True,
                timeout=120,
            )
            if result.returncode != 0:
                raise RuntimeError(f"scp failed for {audio_ref}")

        await asyncio.to_thread(_do_store)
        _logger.info("audio_file_stored_ssh", audio_ref=audio_ref, host=self._host)
        return audio_ref

    async def resolve_local_path(self, audio_ref: str) -> Path | None:
        remote_path = f"{self._remote_base}/{audio_ref}"
        tmp = Path(tempfile.mkdtemp()) / Path(audio_ref).name

        def _do_fetch() -> subprocess.CompletedProcess[bytes]:
            return subprocess.run(
                self._scp_download_cmd(remote_path, str(tmp)),
                capture_output=True,
                timeout=120,
            )

        result = await asyncio.to_thread(_do_fetch)
        if result.returncode != 0:
            _logger.warning(
                "ssh_fetch_failed",
                audio_ref=audio_ref,
                stderr=result.stderr.decode()[:200],
            )
            return None
        _logger.info("audio_fetched_ssh", audio_ref=audio_ref, size=tmp.stat().st_size)
        return tmp

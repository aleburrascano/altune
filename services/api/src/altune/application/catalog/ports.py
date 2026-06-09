"""Catalog ports — interfaces owned by the application layer.

Implementations live in `adapters/outbound/persistence/catalog/`. Tests use
in-memory fakes from `tests/_doubles/`. The use cases in this package call
these interfaces, never the concrete adapters.
"""

from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path
from typing import TYPE_CHECKING, Protocol

if TYPE_CHECKING:
    from collections.abc import Sequence

    from altune.domain.catalog.playlist import Playlist
    from altune.domain.catalog.playlist_id import PlaylistId
    from altune.domain.catalog.track import Track
    from altune.domain.catalog.track_id import TrackId
    from altune.domain.shared.user_id import UserId


@dataclass(frozen=True, slots=True)
class AudioCandidate:
    """A candidate audio result from a search — metadata only, no download yet."""

    title: str
    artist: str
    duration_seconds: int | None
    url: str
    channel: str = ""
    categories: tuple[str, ...] = ()
    view_count: int = 0
    channel_follower_count: int = 0


class AudioSearcher(Protocol):
    """Search for audio candidates and download a selected one."""

    async def search(self, query: str, limit: int = 5) -> list[AudioCandidate]:
        """Search for audio by query string, return candidate metadata."""
        ...

    async def download(self, url: str, temp_dir: Path) -> Path:
        """Download audio from URL to temp_dir, return path to the file."""
        ...


class AudioStore(Protocol):
    """Persist audio files to permanent storage."""

    async def store(self, source_path: Path, audio_ref: str) -> str:
        """Move source_path to permanent storage at audio_ref. Return the ref."""
        ...

    def exists(self, audio_ref: str) -> bool:
        """Check if a stored file exists at audio_ref."""
        ...


class TrackRepository(Protocol):
    """A read+write port over the Track aggregate.

    For view-library (read-only), only `list_for_user` is required; write
    methods land with the future write-path specs (add-track-manually,
    edit-track-metadata, delete-track) so the port grows by feature, not
    pre-emptively.
    """

    async def list_for_user(
        self,
        user_id: UserId,
        limit: int,
        offset: int,
    ) -> tuple[Sequence[Track], int]:
        """Return one page of the user's tracks plus the total count.

        Ordering is ``(added_at DESC, id DESC)`` — id is the stable tiebreaker
        per the spec's AC#1. Implementations enforce user-scoping at the
        storage boundary (e.g., ``WHERE user_id = $1`` in SQL).

        ``total`` is a snapshot at the moment of this call; concurrent writes
        between paged requests can shift values. Acceptable for v1 per the
        spec's Design Considerations.
        """
        ...

    async def get_by_id(self, track_id: TrackId, user_id: UserId) -> Track | None:
        """Return a single track by id, scoped to the user. None if not found."""
        ...

    async def delete(self, track_id: TrackId, user_id: UserId) -> bool:
        """Delete a track by id, scoped to user. Returns True if deleted, False if not found."""
        ...

    async def update(self, track: Track) -> Track:
        """Persist an updated Track instance (e.g., status transition).

        The caller constructs a new frozen Track with the desired state;
        the repository overwrites the persisted row. Raises ValueError
        if the track does not exist.
        """
        ...

    async def add(self, track: Track) -> tuple[Track, bool]:
        """Persist a new track, or return the existing one on a dedup hit.

        Returns ``(persisted_track, created)``. ``created`` is ``False`` when a
        track with the same natural key already exists for the user (the dedup
        key is computed over user_id + normalized title/artist/album): the
        existing track is returned and no duplicate row is written.

        Idempotency is enforced by the ``UNIQUE(user_id, dedup_key)`` constraint
        in the SQL adapter, not by a read-then-write check (which would race).
        Introduced by `docs/specs/view-result-detail/spec.md` (AC#5, AC#7).
        """
        ...


class PlaylistRepository(Protocol):
    """Read+write port over the Playlist aggregate."""

    async def create(self, playlist: Playlist) -> Playlist: ...

    async def list_for_user(self, user_id: UserId) -> Sequence[Playlist]: ...

    async def get_by_id(self, playlist_id: PlaylistId, user_id: UserId) -> Playlist | None: ...

    async def get_with_tracks(
        self,
        playlist_id: PlaylistId,
        user_id: UserId,
    ) -> tuple[Playlist, Sequence[Track]] | None:
        """Return the playlist with its full ordered Track objects."""
        ...

    async def update_name(
        self, playlist_id: PlaylistId, user_id: UserId, name: str
    ) -> Playlist | None: ...

    async def delete(self, playlist_id: PlaylistId, user_id: UserId) -> bool: ...

    async def add_track(
        self,
        playlist_id: PlaylistId,
        user_id: UserId,
        track_id: TrackId,
    ) -> bool:
        """Add a track at the end. Returns False if already present."""
        ...

    async def remove_track(
        self,
        playlist_id: PlaylistId,
        user_id: UserId,
        track_id: TrackId,
    ) -> bool:
        """Remove a track and compact positions. Returns False if not found."""
        ...

    async def reorder_tracks(
        self,
        playlist_id: PlaylistId,
        user_id: UserId,
        track_ids: Sequence[TrackId],
    ) -> bool:
        """Reassign positions 0..N-1 in the given order. Returns False if playlist not found."""
        ...

    async def get_preview_artwork(
        self,
        playlist_id: PlaylistId,
        user_id: UserId,
    ) -> Sequence[str]:
        """Return up to 4 unique artwork URLs from the playlist's tracks."""
        ...

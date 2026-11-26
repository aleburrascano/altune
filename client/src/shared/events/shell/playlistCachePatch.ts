import type {
  PlaylistId,
  TrackId,
  ListPlaylistsResponse,
  PlaylistDetailResponse,
  PlaylistResponse,
  TrackResponse,
} from '@shared/catalog';
import { playlistKeys } from '@shared/query';

import type { EventCache } from '@shared/events/core/eventCache';

interface PlaylistPages {
  pages: ListPlaylistsResponse[];
  pageParams: number[];
}

function revisePlaylistEverywhere(
  cache: EventCache,
  playlistId: PlaylistId,
  revise: (playlist: PlaylistResponse) => PlaylistResponse,
): void {
  const reviseOne = (p: PlaylistResponse): PlaylistResponse =>
    p.id === playlistId ? revise(p) : p;

  cache.setQueryData<ListPlaylistsResponse>(playlistKeys.list, (prev) =>
    prev ? { ...prev, items: prev.items.map(reviseOne) } : prev,
  );
  cache.setQueryData<PlaylistPages>(playlistKeys.paged, (prev) =>
    prev
      ? {
          ...prev,
          pages: prev.pages.map((page) => ({ ...page, items: page.items.map(reviseOne) })),
        }
      : prev,
  );
}

export function patchPlaylistName(cache: EventCache, playlistId: PlaylistId, name: string): void {
  cache.setQueryData<PlaylistDetailResponse>(playlistKeys.detail(playlistId), (prev) =>
    prev ? { ...prev, name } : prev,
  );
  revisePlaylistEverywhere(cache, playlistId, (p) => ({ ...p, name }));
}

function reviseTrackCountEverywhere(
  cache: EventCache,
  playlistId: PlaylistId,
  nextCount: (current: number) => number,
): void {
  revisePlaylistEverywhere(cache, playlistId, (p) => ({
    ...p,
    track_count: nextCount(p.track_count),
  }));
}

function dropTracksFromDetail(
  cache: EventCache,
  playlistId: PlaylistId,
  removedIds: ReadonlySet<string>,
): void {
  cache.setQueryData<PlaylistDetailResponse>(playlistKeys.detail(playlistId), (prev) => {
    if (!prev) return prev;
    const tracks = prev.tracks.filter((t) => !removedIds.has(t.id));
    return { ...prev, tracks, track_count: tracks.length };
  });
}

export function removeTracksFromPlaylistCache(
  cache: EventCache,
  playlistId: PlaylistId,
  trackIds: readonly string[],
): void {
  const removedIds = new Set(trackIds);
  if (removedIds.size === 0) return;
  const before = cache.getQueryData(playlistKeys.detail(playlistId)) as
    PlaylistDetailResponse | undefined;
  dropTracksFromDetail(cache, playlistId, removedIds);
  if (!before) {
    reviseTrackCountEverywhere(cache, playlistId, (count) => Math.max(0, count - removedIds.size));
    return;
  }
  const remainingCount = before.tracks.filter((t) => !removedIds.has(t.id)).length;
  if (remainingCount < before.tracks.length) {
    reviseTrackCountEverywhere(cache, playlistId, () => remainingCount);
  }
}

export function removeTrackFromPlaylistCache(
  cache: EventCache,
  playlistId: PlaylistId,
  trackId: TrackId,
): void {
  removeTracksFromPlaylistCache(cache, playlistId, [trackId]);
}

export function reorderPlaylistCache(
  cache: EventCache,
  playlistId: PlaylistId,
  trackIds: string[],
): void {
  cache.setQueryData<PlaylistDetailResponse>(playlistKeys.detail(playlistId), (prev) => {
    if (!prev) return prev;
    const byId = new Map<string, TrackResponse>(prev.tracks.map((t) => [t.id, t]));
    const namedIds = new Set(trackIds);
    const ordered = [...namedIds]
      .map((id) => byId.get(id))
      .filter((t): t is TrackResponse => t != null);
    const unnamed = prev.tracks.filter((t) => !namedIds.has(t.id));
    return { ...prev, tracks: [...ordered, ...unnamed] };
  });
}

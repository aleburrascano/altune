import type { InfiniteData, QueryClient } from '@tanstack/react-query';

import type { PlaylistId, TrackId } from '@shared/api-client/ids';
import type {
  ListPlaylistsResponse,
  PlaylistDetailResponse,
  PlaylistResponse,
  TrackResponse,
} from '@shared/api-client/types';
import { playlistKeys } from '@shared/lib/query-keys';

/**
 * Applies revise to the playlist wherever the collection is cached: the sheet's single
 * response and the library grid's pages hold the same playlists under two keys, and a
 * patched event never reaches the one it was not written for.
 */
function revisePlaylistEverywhere(
  queryClient: QueryClient,
  playlistId: PlaylistId,
  revise: (playlist: PlaylistResponse) => PlaylistResponse,
): void {
  const reviseOne = (p: PlaylistResponse): PlaylistResponse =>
    p.id === playlistId ? revise(p) : p;

  queryClient.setQueryData<ListPlaylistsResponse>(playlistKeys.list, (prev) =>
    prev ? { ...prev, items: prev.items.map(reviseOne) } : prev,
  );
  queryClient.setQueryData<InfiniteData<ListPlaylistsResponse, number>>(
    playlistKeys.paged,
    (prev) =>
      prev
        ? {
            ...prev,
            pages: prev.pages.map((page) => ({ ...page, items: page.items.map(reviseOne) })),
          }
        : prev,
  );
}

export function patchPlaylistName(
  queryClient: QueryClient,
  playlistId: PlaylistId,
  name: string,
): void {
  queryClient.setQueryData<PlaylistDetailResponse>(playlistKeys.detail(playlistId), (prev) =>
    prev ? { ...prev, name } : prev,
  );
  revisePlaylistEverywhere(queryClient, playlistId, (p) => ({ ...p, name }));
}

function reviseTrackCountEverywhere(
  queryClient: QueryClient,
  playlistId: PlaylistId,
  nextCount: (current: number) => number,
): void {
  revisePlaylistEverywhere(queryClient, playlistId, (p) => ({
    ...p,
    track_count: nextCount(p.track_count),
  }));
}

function dropTracksFromDetail(
  queryClient: QueryClient,
  playlistId: PlaylistId,
  removedIds: ReadonlySet<string>,
): void {
  queryClient.setQueryData<PlaylistDetailResponse>(playlistKeys.detail(playlistId), (prev) => {
    if (!prev) return prev;
    const tracks = prev.tracks.filter((t) => !removedIds.has(t.id));
    return { ...prev, tracks, track_count: tracks.length };
  });
}

/**
 * One read and one write for the whole batch, so a bulk removal from a long playlist stays
 * linear in its length. A cached detail is the authority on what is left; without one the
 * summary caches can only assume every named track really was on the playlist.
 */
export function removeTracksFromPlaylistCache(
  queryClient: QueryClient,
  playlistId: PlaylistId,
  trackIds: readonly string[],
): void {
  const removedIds = new Set(trackIds);
  if (removedIds.size === 0) return;
  const before = queryClient.getQueryData<PlaylistDetailResponse>(playlistKeys.detail(playlistId));
  dropTracksFromDetail(queryClient, playlistId, removedIds);
  if (!before) {
    reviseTrackCountEverywhere(queryClient, playlistId, (count) =>
      Math.max(0, count - removedIds.size),
    );
    return;
  }
  const remainingCount = before.tracks.filter((t) => !removedIds.has(t.id)).length;
  if (remainingCount < before.tracks.length) {
    reviseTrackCountEverywhere(queryClient, playlistId, () => remainingCount);
  }
}

export function removeTrackFromPlaylistCache(
  queryClient: QueryClient,
  playlistId: PlaylistId,
  trackId: TrackId,
): void {
  removeTracksFromPlaylistCache(queryClient, playlistId, [trackId]);
}

export function reorderPlaylistCache(
  queryClient: QueryClient,
  playlistId: PlaylistId,
  trackIds: string[],
): void {
  queryClient.setQueryData<PlaylistDetailResponse>(playlistKeys.detail(playlistId), (prev) => {
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

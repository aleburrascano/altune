import type { InfiniteData, QueryClient } from '@tanstack/react-query';

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
  playlistId: string,
  revise: (playlist: PlaylistResponse) => PlaylistResponse,
): void {
  const reviseOne = (p: PlaylistResponse): PlaylistResponse => (p.id === playlistId ? revise(p) : p);

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
  playlistId: string,
  name: string,
): void {
  queryClient.setQueryData<PlaylistDetailResponse>(playlistKeys.detail(playlistId), (prev) =>
    prev ? { ...prev, name } : prev,
  );
  revisePlaylistEverywhere(queryClient, playlistId, (p) => ({ ...p, name }));
}

export function removeTrackFromPlaylistCache(
  queryClient: QueryClient,
  playlistId: string,
  trackId: string,
): void {
  const before = queryClient.getQueryData<PlaylistDetailResponse>(playlistKeys.detail(playlistId));
  const wasPresent = before?.tracks.some((t) => t.id === trackId);

  queryClient.setQueryData<PlaylistDetailResponse>(playlistKeys.detail(playlistId), (prev) => {
    if (!prev) return prev;
    const tracks = prev.tracks.filter((t) => t.id !== trackId);
    return { ...prev, tracks, track_count: tracks.length };
  });

  if (wasPresent === false) return;

  const authoritativeCount = wasPresent === true ? (before?.tracks.length ?? 1) - 1 : null;

  revisePlaylistEverywhere(queryClient, playlistId, (p) => ({
    ...p,
    track_count: authoritativeCount ?? Math.max(0, p.track_count - 1),
  }));
}

export function reorderPlaylistCache(
  queryClient: QueryClient,
  playlistId: string,
  trackIds: string[],
): void {
  queryClient.setQueryData<PlaylistDetailResponse>(playlistKeys.detail(playlistId), (prev) => {
    if (!prev) return prev;
    const byId = new Map<string, TrackResponse>(prev.tracks.map((t) => [t.id, t]));
    const named = [...new Set(trackIds)];
    const ordered = named.map((id) => byId.get(id)).filter((t): t is TrackResponse => t != null);
    const missing = prev.tracks.filter((t) => !byId.has(t.id) || !named.includes(t.id));
    return { ...prev, tracks: [...ordered, ...missing] };
  });
}

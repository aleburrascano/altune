/**
 * playlistCachePatch — patch playlist caches from server events (F13), so a
 * rename / remove-track / reorder on another device propagates instantly instead
 * of forcing a refetch (or, before F13, not propagating at all).
 *
 * Detail is cached at ['playlist', playlistId] (a PlaylistDetailResponse); the
 * list is ['playlists'] (a ListPlaylistsResponse).
 */

import type { QueryClient } from '@tanstack/react-query';

import type {
  ListPlaylistsResponse,
  PlaylistDetailResponse,
  TrackResponse,
} from '@shared/api-client/types';

export function patchPlaylistName(queryClient: QueryClient, playlistId: string, name: string): void {
  queryClient.setQueryData<PlaylistDetailResponse>(['playlist', playlistId], (prev) =>
    prev ? { ...prev, name } : prev,
  );
  queryClient.setQueryData<ListPlaylistsResponse>(['playlists'], (prev) =>
    prev
      ? { ...prev, items: prev.items.map((p) => (p.id === playlistId ? { ...p, name } : p)) }
      : prev,
  );
}

export function removeTrackFromPlaylistCache(
  queryClient: QueryClient,
  playlistId: string,
  trackId: string,
): void {
  queryClient.setQueryData<PlaylistDetailResponse>(['playlist', playlistId], (prev) => {
    if (!prev) return prev;
    const tracks = prev.tracks.filter((t) => t.id !== trackId);
    return { ...prev, tracks, track_count: tracks.length };
  });
  queryClient.setQueryData<ListPlaylistsResponse>(['playlists'], (prev) =>
    prev
      ? {
          ...prev,
          items: prev.items.map((p) =>
            p.id === playlistId ? { ...p, track_count: Math.max(0, p.track_count - 1) } : p,
          ),
        }
      : prev,
  );
}

export function reorderPlaylistCache(
  queryClient: QueryClient,
  playlistId: string,
  trackIds: string[],
): void {
  queryClient.setQueryData<PlaylistDetailResponse>(['playlist', playlistId], (prev) => {
    if (!prev) return prev;
    const byId = new Map<string, TrackResponse>(prev.tracks.map((t) => [t.id, t]));
    // Follow the new id order; keep any track not named in the event at the end
    // (defensive — the server sends the full order, so this is normally empty).
    const ordered = trackIds.map((id) => byId.get(id)).filter((t): t is TrackResponse => t != null);
    const missing = prev.tracks.filter((t) => !trackIds.includes(t.id));
    return { ...prev, tracks: [...ordered, ...missing] };
  });
}

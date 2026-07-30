import type { QueryClient } from '@tanstack/react-query';

import type {
  ListPlaylistsResponse,
  PlaylistDetailResponse,
  TrackResponse,
} from '@shared/api-client/types';
import { playlistKeys } from '@shared/lib/query-keys';

export function patchPlaylistName(
  queryClient: QueryClient,
  playlistId: string,
  name: string,
): void {
  queryClient.setQueryData<PlaylistDetailResponse>(playlistKeys.detail(playlistId), (prev) =>
    prev ? { ...prev, name } : prev,
  );
  queryClient.setQueryData<ListPlaylistsResponse>(playlistKeys.list, (prev) =>
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
  const before = queryClient.getQueryData<PlaylistDetailResponse>(playlistKeys.detail(playlistId));
  const wasPresent = before?.tracks.some((t) => t.id === trackId);

  queryClient.setQueryData<PlaylistDetailResponse>(playlistKeys.detail(playlistId), (prev) => {
    if (!prev) return prev;
    const tracks = prev.tracks.filter((t) => t.id !== trackId);
    return { ...prev, tracks, track_count: tracks.length };
  });

  if (wasPresent === false) return;

  const authoritativeCount = wasPresent === true ? (before?.tracks.length ?? 1) - 1 : null;

  queryClient.setQueryData<ListPlaylistsResponse>(playlistKeys.list, (prev) =>
    prev
      ? {
          ...prev,
          items: prev.items.map((p) =>
            p.id === playlistId
              ? {
                  ...p,
                  track_count: authoritativeCount ?? Math.max(0, p.track_count - 1),
                }
              : p,
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
  queryClient.setQueryData<PlaylistDetailResponse>(playlistKeys.detail(playlistId), (prev) => {
    if (!prev) return prev;
    const byId = new Map<string, TrackResponse>(prev.tracks.map((t) => [t.id, t]));
    const named = [...new Set(trackIds)];
    const ordered = named.map((id) => byId.get(id)).filter((t): t is TrackResponse => t != null);
    const missing = prev.tracks.filter((t) => !byId.has(t.id) || !named.includes(t.id));
    return { ...prev, tracks: [...ordered, ...missing] };
  });
}

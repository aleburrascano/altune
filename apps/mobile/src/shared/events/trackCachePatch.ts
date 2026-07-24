import type { QueryClient } from '@tanstack/react-query';

import type {
  ListTracksResponse,
  PlaylistDetailResponse,
  TrackResponse,
} from '@shared/api-client/types';
import { libraryKeys, playlistKeys } from '@shared/lib/query-keys';

export function getTrackFromCaches(
  queryClient: QueryClient,
  trackId: string,
): TrackResponse | undefined {
  const home = queryClient.getQueryData<ListTracksResponse>(libraryKeys.home);
  const inHome = home?.items.find((t) => t.id === trackId);
  if (inHome) return inHome;

  const featuring = queryClient.getQueriesData<ListTracksResponse>({
    queryKey: libraryKeys.featuringPrefix,
  });
  for (const [, list] of featuring) {
    const found = list?.items.find((t) => t.id === trackId);
    if (found) return found;
  }

  const playlists = queryClient.getQueriesData<PlaylistDetailResponse>({
    queryKey: playlistKeys.details,
  });
  for (const [, detail] of playlists) {
    const found = detail?.tracks.find((t) => t.id === trackId);
    if (found) return found;
  }
  return undefined;
}

export function upsertTrackInCaches(queryClient: QueryClient, track: TrackResponse): void {
  queryClient.setQueryData<ListTracksResponse>(libraryKeys.home, (prev) => {
    if (!prev) return prev;
    if (prev.items.some((t) => t.id === track.id)) {
      return {
        ...prev,
        items: prev.items.map((t) => (t.id === track.id ? { ...t, ...track } : t)),
      };
    }
    return { ...prev, items: [track, ...prev.items], total: prev.total + 1 };
  });
}

export function removeTrackFromCaches(queryClient: QueryClient, trackId: string): void {
  const removeFromList = (prev: ListTracksResponse | undefined): ListTracksResponse | undefined => {
    if (!prev) return prev;
    const items = prev.items.filter((t) => t.id !== trackId);
    return { ...prev, items, total: prev.total - (items.length < prev.items.length ? 1 : 0) };
  };

  queryClient.setQueryData<ListTracksResponse>(libraryKeys.home, removeFromList);
  queryClient.setQueriesData<ListTracksResponse>(
    { queryKey: libraryKeys.featuringPrefix },
    removeFromList,
  );

  queryClient.setQueriesData<PlaylistDetailResponse>({ queryKey: playlistKeys.details }, (prev) =>
    prev ? { ...prev, tracks: prev.tracks.filter((t) => t.id !== trackId) } : prev,
  );
}

export function patchTrackInCaches(
  queryClient: QueryClient,
  trackId: string,
  patch: Partial<TrackResponse>,
): void {
  const apply = (t: TrackResponse): TrackResponse => (t.id === trackId ? { ...t, ...patch } : t);
  const patchList = (prev: ListTracksResponse | undefined): ListTracksResponse | undefined =>
    prev ? { ...prev, items: prev.items.map(apply) } : prev;

  queryClient.setQueryData<ListTracksResponse>(libraryKeys.home, patchList);
  queryClient.setQueriesData<ListTracksResponse>(
    { queryKey: libraryKeys.featuringPrefix },
    patchList,
  );

  queryClient.setQueriesData<PlaylistDetailResponse>({ queryKey: playlistKeys.details }, (prev) =>
    prev ? { ...prev, tracks: prev.tracks.map(apply) } : prev,
  );
}

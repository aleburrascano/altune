/**
 * patchTrackInCaches — the single source of truth for a track's live state.
 *
 * Applies a partial update to a track wherever it is cached (library-home
 * snapshot, library infinite query, every playlist detail), keyed by id. This
 * is what makes a backend acquisition event flip every screen at once, instead
 * of one screen's query invalidating while others show stale 'pending'.
 */

import type { InfiniteData, QueryClient } from '@tanstack/react-query';

import type {
  ListTracksResponse,
  PlaylistDetailResponse,
  TrackResponse,
} from '@shared/api-client/types';

/**
 * Finds a track by id across the same caches patchTrackInCaches writes, so the
 * download UI can snapshot display metadata (title/artist/artwork) for an event
 * that carries only a track_id. Returns undefined when the track isn't cached
 * anywhere (e.g. a save from Detail before the library was ever loaded).
 */
export function getTrackFromCaches(
  queryClient: QueryClient,
  trackId: string,
): TrackResponse | undefined {
  const home = queryClient.getQueryData<ListTracksResponse>(['library-home']);
  const inHome = home?.items.find((t) => t.id === trackId);
  if (inHome) return inHome;

  const infinite = queryClient.getQueryData<InfiniteData<ListTracksResponse>>(['library']);
  for (const page of infinite?.pages ?? []) {
    const found = page.items.find((t) => t.id === trackId);
    if (found) return found;
  }

  const playlists = queryClient.getQueriesData<PlaylistDetailResponse>({ queryKey: ['playlist'] });
  for (const [, detail] of playlists) {
    const found = detail?.tracks.find((t) => t.id === trackId);
    if (found) return found;
  }
  return undefined;
}

export function patchTrackInCaches(
  queryClient: QueryClient,
  trackId: string,
  patch: Partial<TrackResponse>,
): void {
  const apply = (t: TrackResponse): TrackResponse => (t.id === trackId ? { ...t, ...patch } : t);

  queryClient.setQueryData<ListTracksResponse>(['library-home'], (prev) =>
    prev ? { ...prev, items: prev.items.map(apply) } : prev,
  );

  queryClient.setQueryData<InfiniteData<ListTracksResponse>>(['library'], (prev) =>
    prev ? { ...prev, pages: prev.pages.map((p) => ({ ...p, items: p.items.map(apply) })) } : prev,
  );

  queryClient.setQueriesData<PlaylistDetailResponse>({ queryKey: ['playlist'] }, (prev) =>
    prev ? { ...prev, tracks: prev.tracks.map(apply) } : prev,
  );
}

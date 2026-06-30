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

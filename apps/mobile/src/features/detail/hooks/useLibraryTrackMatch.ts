import { useMemo } from 'react';
import { useQuery } from '@tanstack/react-query';
import type { InfiniteData } from '@tanstack/react-query';

import type { ListTracksResponse, TrackResponse } from '@shared/api-client/types';

import { findTrackInData } from '../helpers/find-track-in-library-cache';

/**
 * Reactively find a saved track by title+artist in the library caches.
 *
 * Subscribes to the ['library-home'] and ['library'] queries (enabled:false — it
 * never fetches, only observes) so a setQueryData patch from an acquisition SSE
 * event re-renders the consumer. This is what lets the track detail's save
 * control + play button advance on completion WITHOUT the library-wide
 * invalidate the completed/failed handlers used to fire (F12).
 */
export function useLibraryTrackMatch(title: string, artist: string | null): TrackResponse | null {
  const { data: home } = useQuery<ListTracksResponse>({
    queryKey: ['library-home'],
    enabled: false,
  });
  const { data: infinite } = useQuery<InfiniteData<ListTracksResponse>>({
    queryKey: ['library'],
    enabled: false,
  });
  return useMemo(
    () => findTrackInData(home, infinite, title, artist),
    [home, infinite, title, artist],
  );
}

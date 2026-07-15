import { useMemo } from 'react';
import { skipToken, useQuery } from '@tanstack/react-query';
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
  // skipToken: these observers only READ the cache (populated by useLibraryHome /
  // the acquisition SSE setQueryData patches) — they must never fetch. Without a
  // queryFn, React Query logs "No queryFn was passed" whenever this renders while
  // the owning query isn't mounted (e.g. cold-start into a detail screen before
  // the Library tab initializes). skipToken is the v5 way to say "never fetch".
  const { data: home } = useQuery<ListTracksResponse>({
    queryKey: ['library-home'],
    queryFn: skipToken,
  });
  const { data: infinite } = useQuery<InfiniteData<ListTracksResponse>>({
    queryKey: ['library'],
    queryFn: skipToken,
  });
  return useMemo(
    () => findTrackInData(home, infinite, title, artist),
    [home, infinite, title, artist],
  );
}

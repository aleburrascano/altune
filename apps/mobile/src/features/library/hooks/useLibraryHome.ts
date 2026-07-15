import { useQuery } from '@tanstack/react-query';

import { getTracks } from '@shared/api-client/tracks';

import { useLibraryGrouping } from './useLibraryGrouping';

const ALL_TRACKS_LIMIT = 2000;
// A slow safety net, not a realtime path (F14): SSE progress/completed/failed
// events already drive the download store + cache patches. Degraded from a
// 5s/2000-row loop to a 60s belt-and-suspenders poll while anything is pending,
// so a missed terminal event still reconciles without the churn.
const PENDING_POLL_MS = 60_000;

export function useLibraryHome() {
  const { data, isLoading, error, refetch } = useQuery({
    queryKey: ['library-home'],
    queryFn: () => getTracks({ limit: ALL_TRACKS_LIMIT, offset: 0 }),
    // SSE patches keep this coherent; don't background-refetch on mount/nav (F15).
    // Pull-to-refresh remains as the manual escape hatch.
    staleTime: Infinity,
    refetchInterval: (query) => {
      const items = query.state.data?.items;
      if (!items) return false;
      const hasPending = items.some((t) => t.acquisition_status === 'pending');
      return hasPending ? PENDING_POLL_MS : false;
    },
  });

  const allTracks = data?.items ?? [];
  const { albums, artists } = useLibraryGrouping(allTracks);

  return {
    allTracks,
    albums,
    artists,
    total: data?.total ?? 0,
    isLoading,
    error: error as Error | null,
    hasPending: allTracks.some((t) => t.acquisition_status === 'pending'),
    refetch: () => {
      void refetch();
    },
  };
}

export type LibraryHomeState = ReturnType<typeof useLibraryHome>;

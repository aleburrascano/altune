import { useQuery } from '@tanstack/react-query';

import { libraryKeys } from '@shared/lib/query-keys';

import { fetchAllTracks } from '../fetch-all-tracks';
import { useLibraryGrouping } from './useLibraryGrouping';

// Pull-to-refresh (the four Library lists) is the manual escape hatch — see
// `ui/refresh.ts`. Everything else reconciles through SSE cache patches.
// A slow safety net, not a realtime path (F14): SSE progress/completed/failed
// events already drive the download store + cache patches. Degraded from a
// 5s/2000-row loop to a 60s belt-and-suspenders poll while anything is pending,
// so a missed terminal event still reconciles without the churn.
const PENDING_POLL_MS = 60_000;

export function useLibraryHome() {
  const { data, isLoading, isRefetching, error, refetch } = useQuery({
    queryKey: libraryKeys.home,
    // Pages to completion — the grouping/search/sort lenses need every track,
    // so a truncated set renders wrong rather than partial (see fetch-all-tracks).
    queryFn: fetchAllTracks,
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
    isRefetching,
    error: error as Error | null,
    hasPending: allTracks.some((t) => t.acquisition_status === 'pending'),
    refetch: () => {
      void refetch();
    },
  };
}

export type LibraryHomeState = ReturnType<typeof useLibraryHome>;

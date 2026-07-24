import { useQuery } from '@tanstack/react-query';

import { libraryKeys } from '@shared/lib/query-keys';

import { fetchAllTracks } from '../fetch-all-tracks';
import { useLibraryGrouping } from './useLibraryGrouping';

const PENDING_POLL_MS = 60_000;

export function useLibraryHome() {
  const { data, isLoading, isRefetching, error, refetch } = useQuery({
    queryKey: libraryKeys.home,
    queryFn: fetchAllTracks,
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

import { useQuery } from '@tanstack/react-query';

import { getTracks } from '@shared/api-client/tracks';

import { useLibraryGrouping } from './useLibraryGrouping';

const ALL_TRACKS_LIMIT = 2000;
const RECENT_COUNT = 5;
const PENDING_POLL_MS = 5000;

export function useLibraryHome() {
  const { data, isLoading, error, refetch } = useQuery({
    queryKey: ['library-home'],
    queryFn: () => getTracks({ limit: ALL_TRACKS_LIMIT, offset: 0 }),
    refetchInterval: (query) => {
      const items = query.state.data?.items;
      if (!items) return false;
      const hasPending = items.some((t) => t.acquisition_status === 'pending');
      return hasPending ? PENDING_POLL_MS : false;
    },
  });

  const allTracks = data?.items ?? [];
  const recentTracks = allTracks.slice(0, RECENT_COUNT);
  const { albums, artists } = useLibraryGrouping(allTracks);

  return {
    allTracks,
    recentTracks,
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

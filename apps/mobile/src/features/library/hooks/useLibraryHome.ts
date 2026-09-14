import {
  keepPreviousData,
  useInfiniteQuery,
  useQuery,
  useQueryClient,
} from '@tanstack/react-query';

import { getLibraryAlbums, getLibraryArtists, type LibrarySort } from '@shared/api-client/library';
import { getAllTracks, getTracks } from '@shared/api-client/tracks';
import type { TrackResponse } from '@shared/api-client/types';
import { libraryKeys } from '@shared/lib/query-keys';

export const TRACKS_PAGE_SIZE = 200;
const PENDING_POLL_MS = 60_000;

export function useLibraryTracks(query: string, sort: LibrarySort, enabled: boolean) {
  const queryClient = useQueryClient();
  const {
    data,
    isLoading,
    isRefetching,
    error,
    isFetchingNextPage,
    hasNextPage,
    fetchNextPage,
    refetch,
  } = useInfiniteQuery({
    queryKey: libraryKeys.tracks(query, sort),
    initialPageParam: 0,
    // Forwarding the signal lets TanStack abort a superseded search's in-flight page when
    // the key changes, instead of it running to its own deadline (#794).
    queryFn: ({ pageParam, signal }) =>
      getTracks({ limit: TRACKS_PAGE_SIZE, offset: pageParam, q: query, sort }, signal),
    getNextPageParam: (lastPage) =>
      lastPage.has_more ? lastPage.offset + lastPage.items.length : undefined,
    enabled,
    staleTime: Infinity,
    placeholderData: keepPreviousData,
    refetchInterval: (q) => {
      const pending = q.state.data?.pages.some((page) =>
        page.items.some((t) => t.acquisition_status === 'pending'),
      );
      return pending === true ? PENDING_POLL_MS : false;
    },
  });

  const pages = data?.pages ?? [];
  const tracks = pages.flatMap((page) => page.items);

  return {
    tracks,
    total: pages[0]?.total ?? 0,
    isLoading,
    isRefetching,
    error: error,
    isFetchingNextPage,
    onEndReached: () => {
      if (hasNextPage && !isFetchingNextPage) void fetchNextPage();
    },
    refetch: () => {
      void refetch();
    },
    loadAll: (): Promise<TrackResponse[]> =>
      queryClient
        .fetchQuery({
          queryKey: libraryKeys.tracksAll(query, sort),
          queryFn: () => getAllTracks({ q: query, sort }),
          staleTime: Infinity,
        })
        // Playing the pages already loaded beats a shuffle/play tap that does nothing,
        // but the degradation to a subset is recorded rather than silent.
        .catch((error: unknown) => {
          console.warn('[library] whole-library fetch failed; using loaded pages', {
            loaded: tracks.length,
            reason: error instanceof Error ? error.name : typeof error,
          });
          return tracks;
        }),
  };
}

export function useLibraryIsEmpty(): boolean {
  const { data } = useQuery({
    queryKey: libraryKeys.summary,
    queryFn: () => getTracks({ limit: 1, offset: 0 }),
    staleTime: Infinity,
  });
  return data !== undefined && data.total === 0;
}

export function useLibraryAlbums(query: string, sort: LibrarySort, enabled: boolean) {
  const { data, isLoading, isRefetching, error, refetch } = useQuery({
    queryKey: libraryKeys.albums(query, sort),
    queryFn: ({ signal }) => getLibraryAlbums({ q: query, sort }, signal),
    enabled,
    staleTime: Infinity,
    placeholderData: keepPreviousData,
  });

  return {
    albums: data?.items ?? [],
    isLoading,
    isRefetching,
    error: error,
    refetch: () => {
      void refetch();
    },
  };
}

export function useLibraryArtists(query: string, sort: LibrarySort, enabled: boolean) {
  const { data, isLoading, isRefetching, error, refetch } = useQuery({
    queryKey: libraryKeys.artists(query, sort),
    queryFn: ({ signal }) => getLibraryArtists({ q: query, sort }, signal),
    enabled,
    staleTime: Infinity,
    placeholderData: keepPreviousData,
  });

  return {
    artists: data?.items ?? [],
    isLoading,
    isRefetching,
    error: error,
    refetch: () => {
      void refetch();
    },
  };
}

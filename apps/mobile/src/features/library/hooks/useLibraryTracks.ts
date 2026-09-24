import {
  keepPreviousData,
  useInfiniteQuery,
  useQueryClient,
  type InfiniteData,
} from '@tanstack/react-query';
import { useEffect } from 'react';

import type { LibrarySort } from '@shared/api-client/library';
import { getAllTracks, getTracks } from '@shared/api-client/tracks';
import type { ListTracksResponse, TrackResponse } from '@shared/api-client/types';
import { libraryKeys } from '@shared/lib/query-keys';

import { useLoggedLibraryQueryFailure } from './useLoggedLibraryQueryFailure';

export const TRACKS_PAGE_SIZE = 200;
const PENDING_POLL_MS = 60_000;
const MAX_PENDING_POLLS = 10;

type TracksData = InfiniteData<ListTracksResponse, number>;

const hasPending = (page: ListTracksResponse) =>
  page.items.some((t) => t.acquisition_status === 'pending');

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
    // A page can report has_more while serving no items (its total disagreeing with the
    // slice it built). The cursor would then advance by zero and re-request the identical
    // offset forever, so an empty page ends the scroll (#1697).
    getNextPageParam: (lastPage) =>
      lastPage.has_more && lastPage.items.length > 0
        ? lastPage.offset + lastPage.items.length
        : undefined,
    enabled,
    staleTime: Infinity,
    placeholderData: keepPreviousData,
  });

  const anyPending = data?.pages.some(hasPending) === true;
  useEffect(() => {
    if (!enabled || !anyPending) return;
    const key = libraryKeys.tracks(query, sort);
    let polls = 0;
    const timer = setInterval(() => {
      polls += 1;
      if (polls >= MAX_PENDING_POLLS) clearInterval(timer);
      const current = queryClient.getQueryData<TracksData>(key);
      for (const page of current?.pages.filter(hasPending) ?? []) {
        void getTracks({ limit: TRACKS_PAGE_SIZE, offset: page.offset, q: query, sort })
          .then((fresh) => {
            queryClient.setQueryData<TracksData>(key, (old) =>
              old
                ? { ...old, pages: old.pages.map((p) => (p.offset === fresh.offset ? fresh : p)) }
                : old,
            );
          })
          .catch(() => undefined);
      }
    }, PENDING_POLL_MS);
    return () => clearInterval(timer);
  }, [enabled, anyPending, query, sort, queryClient]);

  useLoggedLibraryQueryFailure(error, { chip: 'tracks', sort, isSearching: query !== '' });

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

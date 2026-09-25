import {
  keepPreviousData,
  useInfiniteQuery,
  useQueryClient,
  type QueryClient,
  type InfiniteData,
} from '@tanstack/react-query';
import { useEffect } from 'react';

import type { LibrarySort } from '@shared/api-client/library';
import { getAllTracks, getTracks } from '@shared/api-client/tracks';
import type { ListTracksResponse, TrackResponse } from '@shared/api-client/types';
import { libraryKeys } from '@shared/lib/query-keys';

import { failureLogFields } from '../failureLogFields';
import { pagedListControls } from './pagedListControls';
import { useLoggedLibraryQueryFailure } from './useLoggedLibraryQueryFailure';

export const TRACKS_PAGE_SIZE = 200;
const PENDING_POLL_MS = 60_000;
const MAX_PENDING_POLLS = 10;

type TracksData = InfiniteData<ListTracksResponse, number>;

const hasPending = (page: ListTracksResponse) =>
  page.items.some((t) => t.acquisition_status === 'pending');

const mergePage = (old: TracksData | undefined, fresh: ListTracksResponse) =>
  old
    ? { ...old, pages: old.pages.map((p) => (p.offset === fresh.offset ? fresh : p)) }
    : old;

const logPollFailure = (offset: number, cause: unknown) => {
  console.warn('[library] pending poll refresh failed', { offset, ...failureLogFields(cause) });
};

function refreshPage(queryClient: QueryClient, query: string, sort: LibrarySort, offset: number) {
  const key = libraryKeys.tracks(query, sort);
  void getTracks({ limit: TRACKS_PAGE_SIZE, offset, q: query, sort })
    .then((fresh) => {
      queryClient.setQueryData<TracksData>(key, (old) => mergePage(old, fresh));
    })
    .catch((cause: unknown) => {
      logPollFailure(offset, cause);
    });
}

function refreshPending(queryClient: QueryClient, query: string, sort: LibrarySort) {
  const current = queryClient.getQueryData<TracksData>(libraryKeys.tracks(query, sort));
  for (const page of current?.pages.filter(hasPending) ?? []) {
    refreshPage(queryClient, query, sort, page.offset);
  }
}

function startPoll(queryClient: QueryClient, query: string, sort: LibrarySort) {
  let polls = 0;
  const timer = setInterval(() => {
    polls += 1;
    if (polls >= MAX_PENDING_POLLS) clearInterval(timer);
    refreshPending(queryClient, query, sort);
  }, PENDING_POLL_MS);
  return () => clearInterval(timer);
}

function usePendingPoll(active: boolean, query: string, sort: LibrarySort) {
  const queryClient = useQueryClient();
  useEffect(() => {
    if (!active) return;
    return startPoll(queryClient, query, sort);
  }, [active, query, sort, queryClient]);
}

export function useLibraryTracks(query: string, sort: LibrarySort, enabled: boolean) {
  const queryClient = useQueryClient();
  const {
    data: tracksData,
    isLoading,
    isRefetching,
    error,
    isFetchingNextPage,
    isFetchNextPageError,
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

  const anyPending = tracksData?.pages.some(hasPending) === true;
  usePendingPoll(enabled && anyPending, query, sort);

  useLoggedLibraryQueryFailure(error, { chip: 'tracks', sort, isSearching: query !== '' });

  const pages = tracksData?.pages ?? [];
  const tracks = pages.flatMap((page) => page.items);

  return {
    tracks,
    total: pages[0]?.total ?? 0,
    isLoading,
    isRefetching,
    error: error,
    isFetchingNextPage,
    nextPageFailed: isFetchNextPageError,
    onRetryNextPage: () => {
      void fetchNextPage();
    },
    ...pagedListControls({
      hasNextPage,
      isFetchingNextPage,
      isFetchNextPageError,
      fetchNextPage,
      refetch,
    }),
    loadAll: (): Promise<TrackResponse[]> =>
      queryClient
        .fetchQuery({
          queryKey: libraryKeys.tracksAll(query, sort),
          queryFn: () => getAllTracks({ q: query, sort }),
          staleTime: 0,
          gcTime: 0,
        })
        // Playing the pages already loaded beats a shuffle/play tap that does nothing,
        // but the degradation to a subset is recorded rather than silent.
        .catch((error: unknown) => {
          console.warn('[library] whole-library fetch failed; using loaded pages', {
            loaded: tracks.length,
            ...failureLogFields(error),
          });
          return tracks;
        }),
  };
}

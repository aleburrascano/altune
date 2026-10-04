import {
  keepPreviousData,
  useInfiniteQuery,
  useQueryClient,
  type QueryClient,
  type InfiniteData,
} from '@tanstack/react-query';
import { useEffect } from 'react';

import type { LibrarySort } from '@shared/api-client/library';
import type { ListTracksResponse, TrackResponse } from '@shared/api-client/types';
import { libraryKeys } from '@shared/lib/query-keys';

import type { LibraryPort } from '@features/library/ports/libraryPort';
import { useLibraryPort } from '@features/library/ports/LibraryPortProvider';

import { failureLogFields } from '../failureLogFields';
import { classifyLibraryError, type LibraryFailure } from '../state';
import { pagedListControls } from './pagedListControls';
import { useLoggedLibraryQueryFailure } from './useLoggedLibraryQueryFailure';

export const TRACKS_PAGE_SIZE = 200;
const PENDING_POLL_MS = 60_000;
const MAX_PENDING_POLLS = 10;

type TracksData = InfiniteData<ListTracksResponse, number>;

const hasPending = (page: ListTracksResponse) =>
  page.items.some((t) => t.acquisition_status === 'pending');

const mergePage = (old: TracksData | undefined, fresh: ListTracksResponse) =>
  old ? { ...old, pages: old.pages.map((p) => (p.offset === fresh.offset ? fresh : p)) } : old;

const logPollFailure = (offset: number, cause: unknown) => {
  console.warn('[library] pending poll refresh failed', { offset, ...failureLogFields(cause) });
};

type FallbackHandler = (failure: LibraryFailure, loaded: number) => void;

function loadedPagesOnFailure(tracks: TrackResponse[], onFallback: FallbackHandler | undefined) {
  return (error: unknown): TrackResponse[] => {
    console.warn('[library] whole-library fetch failed; using loaded pages', {
      loaded: tracks.length,
      ...failureLogFields(error),
    });
    onFallback?.(classifyLibraryError(error), tracks.length);
    return tracks;
  };
}

interface PollTarget {
  port: LibraryPort;
  queryClient: QueryClient;
  query: string;
  sort: LibrarySort;
  signal: AbortSignal;
}

async function fetchUnlessSuperseded(target: PollTarget, offset: number) {
  const { port, queryClient, query, sort, signal } = target;
  const key = libraryKeys.tracks(query, sort);
  const before = queryClient.getQueryData<TracksData>(key);
  const fresh = await port.getTracks({ limit: TRACKS_PAGE_SIZE, offset, q: query, sort }, signal);
  const superseded = signal.aborted || queryClient.getQueryData<TracksData>(key) !== before;
  return superseded ? undefined : fresh;
}

const writeFresh =
  ({ queryClient, query, sort }: PollTarget) =>
  (fresh?: ListTracksResponse) =>
    fresh &&
    queryClient.setQueryData<TracksData>(libraryKeys.tracks(query, sort), (old) =>
      mergePage(old, fresh),
    );

function refreshPage(target: PollTarget, offset: number) {
  return fetchUnlessSuperseded(target, offset)
    .then(writeFresh(target))
    .catch((cause: unknown) => {
      if (!target.signal.aborted) logPollFailure(offset, cause);
    });
}

const pendingOffsets = ({ queryClient, query, sort }: PollTarget) =>
  (queryClient.getQueryData<TracksData>(libraryKeys.tracks(query, sort))?.pages ?? [])
    .filter(hasPending)
    .map((page) => page.offset);

async function refreshPending(target: PollTarget) {
  for (const offset of pendingOffsets(target)) {
    if (target.signal.aborted) return;
    await refreshPage(target, offset);
  }
}

type PollScope = Omit<PollTarget, 'signal'>;

interface PollBudget {
  polls: number;
  inFlight: boolean;
}

function releaseSlot(budget: PollBudget) {
  budget.inFlight = false;
}

function pollTick(scope: PollScope, signal: AbortSignal, stop: () => void) {
  const budget: PollBudget = { polls: 0, inFlight: false };
  return () => {
    budget.polls += 1;
    if (budget.polls >= MAX_PENDING_POLLS) stop();
    if (budget.inFlight) return;
    budget.inFlight = true;
    void refreshPending({ ...scope, signal }).finally(() => releaseSlot(budget));
  };
}

function startPoll(scope: PollScope) {
  const controller = new AbortController();
  const tick = pollTick(scope, controller.signal, () => clearInterval(timer));
  const timer = setInterval(tick, PENDING_POLL_MS);
  return () => {
    clearInterval(timer);
    controller.abort();
  };
}

function usePendingPoll(active: boolean, query: string, sort: LibrarySort) {
  const port = useLibraryPort();
  const queryClient = useQueryClient();
  useEffect(() => {
    if (!active) return;
    return startPoll({ port, queryClient, query, sort });
  }, [active, query, sort, queryClient, port]);
}

export function useLibraryTracks(query: string, sort: LibrarySort, enabled: boolean) {
  const port = useLibraryPort();
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
    queryFn: ({ pageParam, signal }) =>
      port.getTracks({ limit: TRACKS_PAGE_SIZE, offset: pageParam, q: query, sort }, signal),
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
    loadAll: (onFallback?: FallbackHandler): Promise<TrackResponse[]> =>
      queryClient
        .fetchQuery({
          queryKey: libraryKeys.tracksAll(query, sort),
          queryFn: async ({ signal }) =>
            (await port.getAllTracks({ q: query, sort, signal })).items,
          staleTime: 0,
          gcTime: 0,
        })
        .catch(loadedPagesOnFailure(tracks, onFallback)),
  };
}

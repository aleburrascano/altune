import {
  keepPreviousData,
  useInfiniteQuery,
  useQueryClient,
  type QueryClient,
  type InfiniteData,
} from '@tanstack/react-query';
import { useEffect } from 'react';
import type { LibrarySort } from '@shared/apiClient';
import type { ListTracksResponse, TrackResponse } from '@shared/catalog';
import { libraryKeys, STALE } from '@shared/query';
import type { LibraryPort } from '@features/library/ports/libraryPort';
import { useLibraryPort } from '@features/library/view/LibraryPortProvider';
import {
  failureLogFields,
  classifyLibraryError,
  type LibraryFailure,
  type LibraryWarn,
} from '@features/library/core/libraryFailure';
import { pagedListControls } from '@features/library/core/listPaging';
import { useLoggedLibraryQueryFailure } from '@features/library/view/useLoggedLibraryQueryFailure';
import { isPending } from '@shared/catalog';

const TRACKS_PAGE_SIZE = 200;
const PENDING_POLL_MS = 60_000;
const MAX_PENDING_POLLS = 10;

type TracksData = InfiniteData<ListTracksResponse, number>;

function hasPending(page: ListTracksResponse) {
  return page.items.some((t) => isPending(t.acquisition_status));
}

function mergePage(old: TracksData | undefined, fresh: ListTracksResponse) {
  return old
    ? { ...old, pages: old.pages.map((p) => (p.offset === fresh.offset ? fresh : p)) }
    : old;
}

function logPollFailure(warn: LibraryWarn, offset: number, cause: unknown) {
  warn('[library] pending poll refresh failed', { offset, ...failureLogFields(cause) });
}

type FallbackHandler = (failure: LibraryFailure, loaded: number) => void;

type LoadedPagesFallback = {
  warn: LibraryWarn;
  tracks: TrackResponse[];
  onFallback: FallbackHandler | undefined;
};

function loadedPagesOnFailure({ warn, tracks, onFallback }: LoadedPagesFallback) {
  return (error: unknown): TrackResponse[] => {
    warn('[library] whole-library fetch failed; using loaded pages', {
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

function writeFresh({ queryClient, query, sort }: PollTarget) {
  return (fresh?: ListTracksResponse) =>
    fresh &&
    queryClient.setQueryData<TracksData>(libraryKeys.tracks(query, sort), (old) =>
      mergePage(old, fresh),
    );
}

function refreshPage(target: PollTarget, offset: number) {
  return fetchUnlessSuperseded(target, offset)
    .then(writeFresh(target))
    .catch((cause: unknown) => {
      if (!target.signal.aborted) logPollFailure(target.port.warn, offset, cause);
    });
}

function pendingOffsets({ queryClient, query, sort }: PollTarget): number[] {
  const cached = queryClient.getQueryData<TracksData>(libraryKeys.tracks(query, sort));
  const offsets: number[] = [];
  if (!cached) return offsets;
  for (const page of cached.pages) {
    if (hasPending(page)) offsets.push(page.offset);
  }
  return offsets;
}

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

type LoadedPages = { pages: ListTracksResponse[] };

type LoadedTracks = { tracks: TrackResponse[]; total: number; anyPending: boolean };

function loadedTracks(loaded: LoadedPages | undefined): LoadedTracks {
  if (loaded === undefined) return { tracks: [], total: 0, anyPending: false };
  const { pages } = loaded;
  return {
    tracks: pages.flatMap((page) => page.items),
    total: pages[0]?.total ?? 0,
    anyPending: pages.some(hasPending),
  };
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
    staleTime: STALE.untilEvent,
    placeholderData: keepPreviousData,
  });

  const { tracks, total, anyPending } = loadedTracks(tracksData);
  usePendingPoll(enabled && anyPending, query, sort);

  useLoggedLibraryQueryFailure(error, { chip: 'tracks', sort, isSearching: query !== '' });

  return {
    tracks,
    total,
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
          staleTime: STALE.now,
          gcTime: 0,
        })
        .catch(loadedPagesOnFailure({ warn: port.warn, tracks, onFallback })),
  };
}

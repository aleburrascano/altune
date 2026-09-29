import {
  keepPreviousData,
  useInfiniteQuery,
  type InfiniteData,
  type QueryKey,
  type UseInfiniteQueryResult,
} from '@tanstack/react-query';

import type { LibrarySort } from '@shared/api-client/library';

import { pagedListControls } from './pagedListControls';
import { useLoggedLibraryQueryFailure } from './useLoggedLibraryQueryFailure';
import type { LibraryChip } from '../activeView';
import { GROUP_PAGE_SIZE, nextGroupPageOffset } from '../groupPaging';

interface GroupPage<T> {
  items: T[];
}

interface PagedGroupQueryOptions<T> {
  queryKey: QueryKey;
  chip: LibraryChip;
  query: string;
  sort: LibrarySort;
  enabled: boolean;
  fetchPage: (
    params: { q: string; sort: LibrarySort; limit: number; offset: number },
    signal: AbortSignal,
  ) => Promise<GroupPage<T>>;
}

type PagedQuery<T> = UseInfiniteQueryResult<InfiniteData<GroupPage<T>>>;

const PAGING = {
  initialPageParam: 0,
  getNextPageParam: (lastPage: GroupPage<unknown>, _pages: unknown, lastOffset: number) =>
    nextGroupPageOffset(lastPage.items.length, lastOffset),
  staleTime: Infinity,
  placeholderData: keepPreviousData,
};

const queryStatus = <T>(q: PagedQuery<T>) => ({
  isLoading: q.isLoading,
  isRefetching: q.isRefetching,
  error: q.error,
  isFetchingNextPage: q.isFetchingNextPage,
});

const pageItems = <T>(q: PagedQuery<T>) => ({
  items: q.data?.pages.flatMap((page) => page.items) ?? [],
  nextPageFailed: q.isFetchNextPageError,
  onRetryNextPage: () => {
    void q.fetchNextPage();
  },
});

function useGroupPages<T>(options: PagedGroupQueryOptions<T>) {
  const { queryKey, query, sort, enabled, fetchPage } = options;
  return useInfiniteQuery({
    ...PAGING,
    queryKey,
    enabled,
    queryFn: ({ pageParam, signal }) =>
      fetchPage({ q: query, sort, limit: GROUP_PAGE_SIZE, offset: pageParam }, signal),
  });
}

export function usePagedGroupQuery<T>(options: PagedGroupQueryOptions<T>) {
  const { chip, query, sort } = options;
  const pages = useGroupPages(options);
  useLoggedLibraryQueryFailure(pages.error, { chip, sort, isSearching: query !== '' });
  return { ...queryStatus(pages), ...pageItems(pages), ...pagedListControls(pages) };
}

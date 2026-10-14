import {
  keepPreviousData,
  useInfiniteQuery,
  type InfiniteData,
  type QueryKey,
  type UseInfiniteQueryResult,
} from '@tanstack/react-query';
import type { LibrarySort } from '@shared/apiClient/library';
import {
  pagedListControls,
  GROUP_PAGE_SIZE,
  nextGroupPageOffset,
} from '@features/library/domain/listPaging';
import { useLoggedLibraryQueryFailure } from '@features/library/hooks/useLoggedLibraryQueryFailure';
import type { LibraryChip } from '@features/library/domain/libraryView';
import { libraryKeys } from '@shared/query/queryKeys';
import { useLibraryPort } from '@features/library/ports/LibraryPortProvider';

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

function nextPageOffset<T>(lastPage: GroupPage<T>, _pages: GroupPage<T>[], lastOffset: number) {
  return nextGroupPageOffset(lastPage.items.length, lastOffset);
}

const PAGING = {
  initialPageParam: 0,
  getNextPageParam: nextPageOffset,
  staleTime: Infinity,
  placeholderData: keepPreviousData,
};

function queryStatus<T>(q: PagedQuery<T>) {
  return {
    isLoading: q.isLoading,
    isRefetching: q.isRefetching,
    error: q.error,
    isFetchingNextPage: q.isFetchingNextPage,
  };
}

function pageItems<T>(q: PagedQuery<T>) {
  return {
    items: q.data?.pages.flatMap((page) => page.items) ?? [],
    nextPageFailed: q.isFetchNextPageError,
    onRetryNextPage: () => {
      void q.fetchNextPage();
    },
  };
}

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

function usePagedGroupQuery<T>(options: PagedGroupQueryOptions<T>) {
  const { chip, query, sort } = options;
  const pages = useGroupPages(options);
  useLoggedLibraryQueryFailure(pages.error, { chip, sort, isSearching: query !== '' });
  return { ...queryStatus(pages), ...pageItems(pages), ...pagedListControls(pages) };
}

export function useLibraryAlbums(query: string, sort: LibrarySort, enabled: boolean) {
  const port = useLibraryPort();
  const { items, ...paged } = usePagedGroupQuery({
    queryKey: libraryKeys.albums(query, sort),
    chip: 'albums',
    query,
    sort,
    enabled,
    fetchPage: (params, signal) => port.getLibraryAlbums(params, signal),
  });
  return { albums: items, ...paged };
}

export function useLibraryArtists(query: string, sort: LibrarySort, enabled: boolean) {
  const port = useLibraryPort();
  const { items, ...paged } = usePagedGroupQuery({
    queryKey: libraryKeys.artists(query, sort),
    chip: 'artists',
    query,
    sort,
    enabled,
    fetchPage: (params, signal) => port.getLibraryArtists(params, signal),
  });
  return { artists: items, ...paged };
}

import { keepPreviousData, useInfiniteQuery } from '@tanstack/react-query';

import { getLibraryAlbums, type LibrarySort } from '@shared/api-client/library';
import { libraryKeys } from '@shared/lib/query-keys';

import { pagedListControls } from './pagedListControls';
import { useLoggedLibraryQueryFailure } from './useLoggedLibraryQueryFailure';
import { GROUP_PAGE_SIZE, nextGroupPageOffset } from '../groupPaging';

export function useLibraryAlbums(query: string, sort: LibrarySort, enabled: boolean) {
  const {
    data,
    isLoading,
    isRefetching,
    error,
    isFetchingNextPage,
    isFetchNextPageError,
    hasNextPage,
    fetchNextPage,
    refetch,
  } = useInfiniteQuery({
    queryKey: libraryKeys.albums(query, sort),
    initialPageParam: 0,
    // Forwarding the signal lets TanStack abort a superseded search's in-flight page when
    // the key changes, instead of it running to its own deadline (#794).
    queryFn: ({ pageParam, signal }) =>
      getLibraryAlbums({ q: query, sort, limit: GROUP_PAGE_SIZE, offset: pageParam }, signal),
    getNextPageParam: (lastPage, _pages, lastOffset) =>
      nextGroupPageOffset(lastPage.items.length, lastOffset),
    enabled,
    staleTime: Infinity,
    placeholderData: keepPreviousData,
  });

  useLoggedLibraryQueryFailure(error, { chip: 'albums', sort, isSearching: query !== '' });

  return {
    albums: data?.pages.flatMap((page) => page.items) ?? [],
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
  };
}

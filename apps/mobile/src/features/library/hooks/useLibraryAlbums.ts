import { keepPreviousData, useQuery } from '@tanstack/react-query';

import { getLibraryAlbums, type LibrarySort } from '@shared/api-client/library';
import { libraryKeys } from '@shared/lib/query-keys';

import { useLoggedLibraryQueryFailure } from './useLoggedLibraryQueryFailure';

export function useLibraryAlbums(query: string, sort: LibrarySort, enabled: boolean) {
  const { data, isLoading, isRefetching, error, refetch } = useQuery({
    queryKey: libraryKeys.albums(query, sort),
    queryFn: ({ signal }) => getLibraryAlbums({ q: query, sort }, signal),
    enabled,
    staleTime: Infinity,
    placeholderData: keepPreviousData,
  });

  useLoggedLibraryQueryFailure(error, { chip: 'albums', sort, isSearching: query !== '' });

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

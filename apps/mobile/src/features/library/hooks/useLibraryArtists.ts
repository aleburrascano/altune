import { keepPreviousData, useQuery } from '@tanstack/react-query';

import { getLibraryArtists, type LibrarySort } from '@shared/api-client/library';
import { libraryKeys } from '@shared/lib/query-keys';

import { useLoggedLibraryQueryFailure } from './useLoggedLibraryQueryFailure';

export function useLibraryArtists(query: string, sort: LibrarySort, enabled: boolean) {
  const { data, isLoading, isRefetching, error, refetch } = useQuery({
    queryKey: libraryKeys.artists(query, sort),
    queryFn: ({ signal }) => getLibraryArtists({ q: query, sort }, signal),
    enabled,
    staleTime: Infinity,
    placeholderData: keepPreviousData,
  });

  useLoggedLibraryQueryFailure(error, { chip: 'artists', sort, isSearching: query !== '' });

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

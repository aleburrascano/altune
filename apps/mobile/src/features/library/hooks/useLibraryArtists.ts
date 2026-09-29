import { getLibraryArtists, type LibrarySort } from '@shared/api-client/library';
import { libraryKeys } from '@shared/lib/query-keys';

import { usePagedGroupQuery } from './usePagedGroupQuery';

export function useLibraryArtists(query: string, sort: LibrarySort, enabled: boolean) {
  const { items, ...paged } = usePagedGroupQuery({
    queryKey: libraryKeys.artists(query, sort),
    chip: 'artists',
    query,
    sort,
    enabled,
    fetchPage: getLibraryArtists,
  });
  return { artists: items, ...paged };
}

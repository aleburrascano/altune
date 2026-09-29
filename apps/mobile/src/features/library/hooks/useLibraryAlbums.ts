import { getLibraryAlbums, type LibrarySort } from '@shared/api-client/library';
import { libraryKeys } from '@shared/lib/query-keys';

import { usePagedGroupQuery } from './usePagedGroupQuery';

export function useLibraryAlbums(query: string, sort: LibrarySort, enabled: boolean) {
  const { items, ...paged } = usePagedGroupQuery({
    queryKey: libraryKeys.albums(query, sort),
    chip: 'albums',
    query,
    sort,
    enabled,
    fetchPage: getLibraryAlbums,
  });
  return { albums: items, ...paged };
}

import type { LibrarySort } from '@shared/api-client/library';
import { libraryKeys } from '@shared/lib/query-keys';

import { useLibraryPort } from '@features/library/ports/LibraryPortProvider';

import { usePagedGroupQuery } from './usePagedGroupQuery';

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

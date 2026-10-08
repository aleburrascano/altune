import type { LibrarySort } from '@shared/api-client/library';
import { libraryKeys } from '@shared/lib/query-keys';

import { useLibraryPort } from '@features/library/ports/LibraryPortProvider';

import { usePagedGroupQuery } from '@features/library/hooks/usePagedGroupQuery';

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

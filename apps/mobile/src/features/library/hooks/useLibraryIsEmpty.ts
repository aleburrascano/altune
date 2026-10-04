import { useQuery } from '@tanstack/react-query';

import { libraryKeys } from '@shared/lib/query-keys';

import { useLibraryPort } from '@features/library/ports/LibraryPortProvider';

export function useLibraryIsEmpty(): boolean {
  const port = useLibraryPort();
  const { data } = useQuery({
    queryKey: libraryKeys.summary,
    queryFn: () => port.getTracks({ limit: 1, offset: 0 }),
    staleTime: Infinity,
  });
  return data !== undefined && data.total === 0;
}

import { useQuery } from '@tanstack/react-query';

import { getTracks } from '@shared/api-client/tracks';
import { libraryKeys } from '@shared/lib/query-keys';

export function useLibraryIsEmpty(): boolean {
  const { data } = useQuery({
    queryKey: libraryKeys.summary,
    queryFn: () => getTracks({ limit: 1, offset: 0 }),
    staleTime: Infinity,
  });
  return data !== undefined && data.total === 0;
}

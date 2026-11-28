import { useQuery } from '@tanstack/react-query';

import { libraryKeys, STALE } from '@shared/query';

import { useLibraryPort } from '@features/library/view/LibraryProvider';

export function useLibraryIsEmpty(): boolean {
  const port = useLibraryPort();
  const { data } = useQuery({
    queryKey: libraryKeys.summary,
    queryFn: ({ signal }) => port.getTracks({ limit: 1, offset: 0 }, signal),
    staleTime: STALE.untilEvent,
  });
  return data?.total === 0;
}

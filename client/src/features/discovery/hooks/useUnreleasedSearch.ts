import { useQuery } from '@tanstack/react-query';

import { searchUnreleasedTracks, type UnreleasedSearchHit } from '@shared/apiClient/enrichment';
import { discoveryKeys } from '@shared/query/queryKeys';

import { isSearchableQuery, normalizeQuery } from '@features/discovery/domain/searchPaging';
import { useDiscoverFetchEnabled } from '@features/discovery/hooks/useDiscoverFetchGate';
import { useReportQueryFailure } from '@features/discovery/hooks/useReportQueryFailure';

const NO_HITS: UnreleasedSearchHit[] = [];
const UNRELEASED_SEARCH_STALE_MS = 60_000;

export function useUnreleasedSearch(committedQuery: string): UnreleasedSearchHit[] {
  const trimmed = normalizeQuery(committedQuery);
  const isGateOpen = useDiscoverFetchEnabled();
  const { data, error } = useQuery({
    queryKey: discoveryKeys.unreleasedSearch(trimmed),
    queryFn: ({ signal }) => searchUnreleasedTracks(trimmed, signal),
    enabled: isGateOpen && isSearchableQuery(trimmed),
    staleTime: UNRELEASED_SEARCH_STALE_MS,
    retry: false,
  });
  useReportQueryFailure(error, 'search');
  return data?.items ?? NO_HITS;
}

import { useQuery, type QueryFunctionContext } from '@tanstack/react-query';

import { searchUnreleasedTracks, type UnreleasedSearchHit } from '@shared/apiClient/enrichment';
import { discoveryKeys } from '@shared/query/queryKeys';

import { isSearchableQuery, normalizeQuery } from '@features/discovery/domain/searchPaging';
import { useDiscoverFetchEnabled } from '@features/discovery/hooks/useDiscoverFetchGate';
import { useReportQueryFailure } from '@features/discovery/hooks/useReportQueryFailure';

const NO_HITS: UnreleasedSearchHit[] = [];
const UNRELEASED_SEARCH_STALE_MS = 60_000;

function unreleasedSearchOptions(trimmed: string, isGateOpen: boolean) {
  return {
    queryKey: discoveryKeys.unreleasedSearch(trimmed),
    queryFn: ({ signal }: QueryFunctionContext) => searchUnreleasedTracks(trimmed, signal),
    enabled: isGateOpen && isSearchableQuery(trimmed),
    staleTime: UNRELEASED_SEARCH_STALE_MS,
    retry: false,
  };
}

export function useUnreleasedSearch(committedQuery: string): UnreleasedSearchHit[] {
  const trimmed = normalizeQuery(committedQuery);
  const options = unreleasedSearchOptions(trimmed, useDiscoverFetchEnabled());
  const { data: response, error } = useQuery(options);
  useReportQueryFailure(error, 'search');
  return response?.items ?? NO_HITS;
}

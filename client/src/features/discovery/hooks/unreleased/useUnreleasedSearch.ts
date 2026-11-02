import { useQuery, type QueryFunctionContext } from '@tanstack/react-query';

import type { UnreleasedSearchHit } from '@shared/apiClient/enrichment';
import { discoveryKeys } from '@shared/query/queryKeys';

import { isSearchableQuery, normalizeQuery } from '@features/discovery/domain/searchPaging';
import { useDiscoverFetchEnabled } from '@features/discovery/hooks/search/useDiscoverFetchGate';
import { useReportQueryFailure } from '@features/discovery/hooks/search/useReportQueryFailure';
import type { DiscoverPort } from '@features/discovery/ports/discoverPort';
import { useDiscoverPort } from '@features/discovery/ports/DiscoverPortProvider';

const NO_HITS: UnreleasedSearchHit[] = [];
const UNRELEASED_SEARCH_STALE_MS = 60_000;

function unreleasedSearchOptions(trimmed: string, port: DiscoverPort, isGateOpen: boolean) {
  return {
    queryKey: discoveryKeys.unreleasedSearch(trimmed),
    queryFn: ({ signal }: QueryFunctionContext) => port.searchUnreleasedTracks(trimmed, signal),
    enabled: isGateOpen && isSearchableQuery(trimmed),
    staleTime: UNRELEASED_SEARCH_STALE_MS,
    retry: false,
  };
}

export function useUnreleasedSearch(committedQuery: string): UnreleasedSearchHit[] {
  const trimmed = normalizeQuery(committedQuery);
  const options = unreleasedSearchOptions(trimmed, useDiscoverPort(), useDiscoverFetchEnabled());
  const { data: response, error } = useQuery(options);
  useReportQueryFailure(error, 'unreleased_search');
  return response?.items ?? NO_HITS;
}

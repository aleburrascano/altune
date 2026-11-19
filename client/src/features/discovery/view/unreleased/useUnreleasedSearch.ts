import { useQuery, type QueryFunctionContext } from '@tanstack/react-query';

import type { UnreleasedSearchHit } from '@shared/apiClient';
import { discoveryKeys } from '@shared/query';

import { isSearchableQuery, normalizeQuery } from '@features/discovery/core/searchPaging';
import { useReportQueryFailure } from '@features/discovery/view/search/useReportQueryFailure';
import type { DiscoverPort } from '@features/discovery/ports/discoverPort';
import { useDiscoverPort } from '@features/discovery/view/DiscoverPortProvider';

const NO_HITS: UnreleasedSearchHit[] = [];
const UNRELEASED_SEARCH_STALE_MS = 60_000;

function unreleasedSearchOptions(trimmed: string, port: DiscoverPort) {
  return {
    queryKey: discoveryKeys.unreleasedSearch(trimmed),
    queryFn: ({ signal }: QueryFunctionContext) => port.searchUnreleasedTracks(trimmed, signal),
    enabled: isSearchableQuery(trimmed),
    staleTime: UNRELEASED_SEARCH_STALE_MS,
    retry: false,
  };
}

export function useUnreleasedSearch(committedQuery: string): UnreleasedSearchHit[] {
  const trimmed = normalizeQuery(committedQuery);
  const options = unreleasedSearchOptions(trimmed, useDiscoverPort());
  const { data: response, error } = useQuery(options);
  useReportQueryFailure(error, 'unreleased_search');
  return response?.items ?? NO_HITS;
}

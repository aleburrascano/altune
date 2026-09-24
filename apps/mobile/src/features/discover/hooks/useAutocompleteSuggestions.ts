import { useEffect, useState } from 'react';
import { useQuery } from '@tanstack/react-query';

import { suggestDiscovery } from '@shared/api-client/discovery';

import { discoveryKeys } from '@shared/lib/query-keys';
import { useReportQueryFailure } from '@shared/telemetry/useReportQueryFailure';
import { useDiscoverFetchEnabled } from './discoverFetchGate';
import { isSearchableQuery } from '../searchLimits';

// Suggestions lead the 300ms search commit instead of riding it, so they settle
// on their own shorter pause: long enough that a burst of keystrokes costs one
// request, short enough that the dropdown still beats the results (#1682).
export const SUGGEST_DEBOUNCE_MS = 150;
export const SEARCH_DEBOUNCE_MS = 300;

const SUGGESTION_LIMIT = 5;

export function useAutocompleteSuggestions(inputValue: string) {
  const debouncedQuery = useSettledValue(inputValue.trim().toLowerCase(), SUGGEST_DEBOUNCE_MS);
  const isSuggestEnabled = useDiscoverFetchEnabled();

  const { data, error } = useQuery({
    queryKey: discoveryKeys.suggest(debouncedQuery),
    // Consuming the signal is what lets react-query abort this request once a
    // later query supersedes it; drop it and the superseded fetch runs on.
    queryFn: ({ signal }) =>
      suggestDiscovery({ q: debouncedQuery, limit: SUGGESTION_LIMIT }, signal),
    enabled: isSearchableQuery(debouncedQuery) && isSuggestEnabled,
    staleTime: 60 * 1000,
  });

  useReportQueryFailure(error, 'suggest');

  return { data, error };
}

// Starts settled so a restored input still asks on its first render.
function useSettledValue(value: string, delayMs: number): string {
  const [settled, setSettled] = useState(value);

  useEffect(() => {
    const timer = setTimeout(() => setSettled(value), delayMs);
    return () => clearTimeout(timer);
  }, [value, delayMs]);

  return settled;
}

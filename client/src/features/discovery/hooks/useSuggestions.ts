import { useEffect, useState, type Dispatch, type SetStateAction } from 'react';
import { useQuery } from '@tanstack/react-query';

import { discoveryKeys } from '@shared/query/queryKeys';
import { useReportQueryFailure } from '@shared/telemetry/useReportQueryFailure';
import { useDiscoverFetchEnabled } from '@features/discovery/hooks/useDiscoverFetchGate';
import { isSearchableQuery, normalizeQuery } from '@features/discovery/domain/searchPaging';
import { useDiscoverPort } from '@features/discovery/ports/DiscoverPortProvider';

export const SUGGEST_DEBOUNCE_MS = 150;
export const SEARCH_DEBOUNCE_MS = 300;

const SUGGESTION_LIMIT = 5;

export function useAutocompleteSuggestions(inputValue: string) {
  const debouncedQuery = useSettledValue(
    normalizeQuery(inputValue).toLowerCase(),
    SUGGEST_DEBOUNCE_MS,
  );
  const port = useDiscoverPort();
  const isSuggestEnabled = useDiscoverFetchEnabled();

  const { data, error } = useQuery({
    queryKey: discoveryKeys.suggest(debouncedQuery),
    queryFn: ({ signal }) =>
      port.suggestDiscovery({ q: debouncedQuery, limit: SUGGESTION_LIMIT }, signal),
    enabled: isSearchableQuery(debouncedQuery) && isSuggestEnabled,
    staleTime: 60 * 1000,
    retry: false,
  });

  useReportQueryFailure(error, 'suggest');

  return { data, error };
}

function useSettledValue(value: string, delayMs: number): string {
  const [settled, setSettled] = useState(value);

  useEffect(() => {
    const timer = setTimeout(() => setSettled(value), delayMs);
    return () => clearTimeout(timer);
  }, [value, delayMs]);

  return settled;
}

type SearchInput = {
  inputValue: string;
  onChangeText: (text: string) => void;
  onSubmit: () => void;
  setQuery: (query: string) => void;
};

type SuggestionVisibility = {
  isFocused: boolean;
  setIsFocused: Dispatch<SetStateAction<boolean>>;
  showSuggestions: boolean;
  onChangeText: (text: string) => void;
  onSubmit: () => void;
  onSuggestionSelect: (text: string) => void;
};

export function useSuggestionVisibility(
  search: SearchInput,
  suggestionCount: number,
): SuggestionVisibility {
  const [isFocused, setIsFocused] = useState(false);
  const [suggestionsHidden, setSuggestionsHidden] = useState(false);
  const showSuggestions =
    isFocused && !suggestionsHidden && isSearchableQuery(search.inputValue) && suggestionCount > 0;
  return {
    isFocused,
    setIsFocused,
    showSuggestions,
    onChangeText: (text) => {
      setSuggestionsHidden(false);
      search.onChangeText(text);
    },
    onSubmit: () => {
      setSuggestionsHidden(true);
      search.onSubmit();
    },
    onSuggestionSelect: (text) => {
      setSuggestionsHidden(true);
      search.setQuery(text);
    },
  };
}

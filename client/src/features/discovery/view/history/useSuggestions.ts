import { useState, type Dispatch, type SetStateAction } from 'react';
import { useQuery, type QueryFunctionContext } from '@tanstack/react-query';
import type { DiscoverySuggestion, DiscoverySuggestResponse } from '@shared/catalog';
import { useDebouncedValue } from '@shared/concurrency';
import { discoveryKeys } from '@shared/query';
import { useReportQueryFailure } from '@features/discovery/view/search/useReportQueryFailure';
import { isSearchableQuery, normalizeQuery } from '@features/discovery/core/searchPaging';
import { useDiscoverPort } from '@features/discovery/view/DiscoverPortProvider';
import type { DiscoverPort } from '@features/discovery/ports/discoverPort';

const SUGGEST_DEBOUNCE_MS = 150;
export const SEARCH_DEBOUNCE_MS = 300;

const SUGGESTION_LIMIT = 5;

const SUGGEST_STALE_MS = 60_000;

const NO_SUGGESTIONS: DiscoverySuggestion[] = [];

export function visibleSuggestions(
  canShow: boolean,
  response: DiscoverySuggestResponse | undefined,
): DiscoverySuggestion[] {
  if (!canShow || response === undefined) return NO_SUGGESTIONS;
  return response.suggestions;
}

export function useAutocompleteSuggestions(inputValue: string, canShow = true) {
  const typedQuery = normalizeQuery(inputValue).toLowerCase();
  const debouncedQuery = useDebouncedValue(typedQuery, SUGGEST_DEBOUNCE_MS);
  const isSettled = debouncedQuery === typedQuery;
  const { data: response, error } = useSuggestQuery(debouncedQuery, canShow && isSettled);
  useReportQueryFailure(error, 'suggest');
  return { data: response, error };
}

function useSuggestQuery(query: string, wanted: boolean) {
  const port = useDiscoverPort();
  return useQuery(suggestQueryOptions(port, query, wanted));
}

function suggestQueryOptions(port: DiscoverPort, query: string, wanted: boolean) {
  return {
    queryKey: discoveryKeys.suggest(query),
    queryFn: ({ signal }: QueryFunctionContext) =>
      port.suggestDiscovery({ q: query, limit: SUGGESTION_LIMIT }, signal),
    enabled: wanted && isSearchableQuery(query),
    staleTime: SUGGEST_STALE_MS,
    retry: false,
  };
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
  canShowSuggestions: boolean;
  onChangeText: (text: string) => void;
  onSubmit: () => void;
  onSuggestionSelect: (text: string) => void;
};

type SuggestionHandlers = Pick<
  SuggestionVisibility,
  'onChangeText' | 'onSubmit' | 'onSuggestionSelect'
>;

export function useSuggestionVisibility(search: SearchInput): SuggestionVisibility {
  const [isFocused, setIsFocused] = useState(false);
  const [suggestionsHidden, setSuggestionsHidden] = useState(false);
  const canShowSuggestions =
    isFocused && !suggestionsHidden && isSearchableQuery(search.inputValue);
  const handlers = suggestionHandlers(search, setSuggestionsHidden);
  return { isFocused, setIsFocused, canShowSuggestions, ...handlers };
}

type SetHidden = Dispatch<SetStateAction<boolean>>;

function suggestionHandlers(search: SearchInput, setHidden: SetHidden): SuggestionHandlers {
  return {
    onChangeText: hidingThen(setHidden, false, search.onChangeText),
    onSubmit: hidingThen(setHidden, true, search.onSubmit),
    onSuggestionSelect: hidingThen(setHidden, true, search.setQuery),
  };
}

function hidingThen<T extends unknown[]>(
  setHidden: SetHidden,
  hidden: boolean,
  act: (...args: T) => void,
): (...args: T) => void {
  return (...args) => {
    setHidden(hidden);
    act(...args);
  };
}

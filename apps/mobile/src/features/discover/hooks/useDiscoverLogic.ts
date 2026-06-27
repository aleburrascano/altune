/**
 * useDiscoverLogic — the search machine behind DiscoverScreen.
 *
 * Owns the debounced search, suggestion/focus state, history + click
 * tracking, cross-navigation search-state persistence, and every handler the
 * screen wires up. DiscoverScreen is left as a presentational shell over the
 * returned state. Lifted out of the screen so the orchestration is one unit.
 */
import { useEffect, useRef, useState, type Dispatch, type SetStateAction } from 'react';
import { useRouter } from 'expo-router';
import { Keyboard } from 'react-native';
import { useQueryClient } from '@tanstack/react-query';

import { setSearchState } from '../search-state';
import { useDebouncedSearch } from './useDebouncedSearch';
import { useDiscoverSearch } from './useDiscoverSearch';
import { useAutocompleteSuggestions } from './useAutocompleteSuggestions';
import { useRecordEvent } from '@shared/telemetry/useRecordEvent';
import { useSearchHistory } from './useSearchHistory';
import { stashHandoffForDetail } from '../tap';
import { _viewForState } from '../state';
import type {
  DiscoveryKind,
  DiscoveryResult,
  DiscoverySearchResponse,
  DiscoverySuggestion,
  SearchHistoryItem,
} from '../../../shared/api-client/discovery';
import type { DiscoverView } from '../state';

export type ResultsFilter = 'all' | DiscoveryKind;

export type DiscoverLogic = {
  inputValue: string;
  committedQuery: string;
  pending: boolean;
  onChangeText: (text: string) => void;
  onSubmit: () => void;
  onClear: () => void;
  isFocused: boolean;
  setIsFocused: Dispatch<SetStateAction<boolean>>;
  showSuggestions: boolean;
  suggestionItems: DiscoverySuggestion[];
  onSuggestionSelect: (text: string) => void;
  view: DiscoverView;
  searchData: DiscoverySearchResponse | undefined;
  history: ReturnType<typeof useSearchHistory>;
  filter: ResultsFilter;
  setFilter: Dispatch<SetStateAction<ResultsFilter>>;
  onHistoryTap: (item: SearchHistoryItem) => void;
  onResultTap: (result: DiscoveryResult, position: number) => void;
  onRetry: () => void;
  onRefresh: () => void;
  isRefreshing: boolean;
  correctedQuery: string | undefined;
  originalQuery: string | undefined;
  onSearchOriginal: () => void;
  onClearHistory: () => void;
};

export function useDiscoverLogic(): DiscoverLogic {
  const router = useRouter();
  const search = useDebouncedSearch({ debounceMs: 300, minChars: 2 });
  const [filter, setFilter] = useState<ResultsFilter>('all');
  const queryClient = useQueryClient();
  const { data: searchData, isLoading: isSearching, error: searchError, refetch } = useDiscoverSearch(search.committedQuery, search.isExplicitSubmit);
  const suggestions = useAutocompleteSuggestions(search.inputValue);
  const history = useSearchHistory();
  const recordEvent = useRecordEvent();
  const [isFocused, setIsFocused] = useState(false);
  const [suggestionsHidden, setSuggestionsHidden] = useState(false);

  const showSuggestions = isFocused
    && !suggestionsHidden
    && search.inputValue.trim().length >= 2
    && (suggestions.data?.suggestions?.length ?? 0) > 0;

  useEffect(() => {
    setSearchState(search.committedQuery, search.inputValue);
  }, [search.committedQuery, search.inputValue]);

  useEffect(() => {
    if (searchData) {
      void queryClient.invalidateQueries({ queryKey: ['discovery', 'history'] });
    }
  }, [searchData, queryClient]);

  const prevQueryRef = useRef(search.committedQuery);
  if (prevQueryRef.current !== search.committedQuery) {
    prevQueryRef.current = search.committedQuery;
    setFilter('all');
  }

  const view = _viewForState({
    query: search.committedQuery,
    isLoading: isSearching,
    data: searchData,
    error: searchError as Error | null,
  });

  const onHistoryTap = (item: SearchHistoryItem): void => {
    search.setQuery(item.query);
  };
  const onClearHistory = (): void => {
    queryClient.setQueryData(['discovery', 'history'], { items: [] });
  };
  const onResultTap = (result: DiscoveryResult, position: number): void => {
    Keyboard.dismiss();
    recordEvent.mutate({
      type: 'result_clicked',
      query_norm: searchData?.query_norm ?? search.committedQuery,
      payload: {
        kind: result.kind,
        title: result.title,
        subtitle: result.subtitle ?? null,
        position,
        confidence: result.confidence,
      },
    });
    router.push(stashHandoffForDetail(result));
  };
  const onChangeText = (text: string): void => {
    setSuggestionsHidden(false);
    search.onChangeText(text);
  };
  const onSubmit = (): void => {
    setSuggestionsHidden(true);
    search.onSubmit();
  };
  const onRetry = (): void => {
    search.setQuery(search.committedQuery.trim() || search.committedQuery);
  };
  const onSuggestionSelect = (text: string): void => {
    setSuggestionsHidden(true);
    search.setQuery(text);
  };
  const onSearchOriginal = (): void => {
    if (searchData?.original_query) {
      search.setQuery(searchData.original_query);
    }
  };

  return {
    inputValue: search.inputValue,
    committedQuery: search.committedQuery,
    pending: search.inputValue.trim().length >= 2 && search.inputValue.trim() !== search.committedQuery,
    onChangeText,
    onSubmit,
    onClear: search.onClear,
    isFocused,
    setIsFocused,
    showSuggestions,
    suggestionItems: suggestions.data?.suggestions ?? [],
    onSuggestionSelect,
    view,
    searchData,
    history,
    filter,
    setFilter,
    onHistoryTap,
    onResultTap,
    onRetry,
    onRefresh: () => { void refetch(); },
    isRefreshing: isSearching && searchData !== undefined,
    correctedQuery: searchData?.corrected_query,
    originalQuery: searchData?.original_query,
    onSearchOriginal,
    onClearHistory,
  };
}

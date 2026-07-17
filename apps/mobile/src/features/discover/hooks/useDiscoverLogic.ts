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
import { useMutation, useQueryClient } from '@tanstack/react-query';

import { clearSearchHistory } from '@shared/api-client/discovery';
import { discoveryKeys } from '../keys';
import { setSearchState } from '../search-state';
import { useDebouncedSearch } from './useDebouncedSearch';
import { useDiscoverSearch } from './useDiscoverSearch';
import { useAutocompleteSuggestions } from './useAutocompleteSuggestions';
import { useImpressionLogger, type ImpressionHandlers } from './useImpressionLogger';
import { useRecordEvent } from '@shared/telemetry/useRecordEvent';
import { useSearchHistory } from './useSearchHistory';
import { stashHandoffForDetail } from '../tap';
import { _viewForState } from '../state';
import type {
  DiscoveryResult,
  DiscoverySearchResponse,
  DiscoverySuggestion,
  SearchHistoryItem,
} from '@shared/api-client/discovery';
import type { DiscoverView, ResultsFilter } from '../state';

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
  historyItems: SearchHistoryItem[];
  filter: ResultsFilter;
  setFilter: Dispatch<SetStateAction<ResultsFilter>>;
  onHistoryTap: (item: SearchHistoryItem) => void;
  onResultTap: (result: DiscoveryResult, position: number) => void;
  impression: ImpressionHandlers;
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
  const impression = useImpressionLogger(searchData);
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
      void queryClient.invalidateQueries({ queryKey: discoveryKeys.history });
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
  // Clearing recent searches must delete the rows server-side, not just the
  // cache — otherwise they reappear on the next mount when useSearchHistory
  // refetches. Optimistically empty the cache for an instant UI; on failure,
  // invalidate to restore the true (still-populated) history.
  const clearHistoryMutation = useMutation({
    mutationFn: clearSearchHistory,
    onError: () => {
      void queryClient.invalidateQueries({ queryKey: discoveryKeys.history });
    },
  });
  const onClearHistory = (): void => {
    queryClient.setQueryData(discoveryKeys.history, { items: [] });
    clearHistoryMutation.mutate();
  };
  const onResultTap = (result: DiscoveryResult, position: number): void => {
    Keyboard.dismiss();
    // Telemetry position is the GLOBAL rank in results[] — the same coordinate
    // space buildImpressionRows logs on results_shown — so CTR@position joins
    // line up. The incoming `position` is the row's section-/filter-local index
    // (display + testID only); it's only a fallback for results not in the slate.
    const globalIndex = searchData?.results.indexOf(result) ?? -1;
    recordEvent.mutate({
      type: 'result_clicked',
      query_norm: searchData?.query_norm ?? search.committedQuery,
      search_id: searchData?.search_id,
      payload: {
        kind: result.kind,
        title: result.title,
        subtitle: result.subtitle ?? null,
        position: globalIndex >= 0 ? globalIndex : position,
        confidence: result.confidence,
        provider: result.sources[0]?.provider ?? null,
        result_signature: result.result_signature ?? null,
      },
    });
    router.push(stashHandoffForDetail(result, searchData?.search_id));
  };
  const onChangeText = (text: string): void => {
    setSuggestionsHidden(false);
    search.onChangeText(text);
  };
  const onSubmit = (): void => {
    setSuggestionsHidden(true);
    search.onSubmit();
  };
  // Refetch directly: re-setting the same committedQuery doesn't change the
  // query key, so React Query would never re-run a failed query from it.
  const onRetry = (): void => {
    void refetch();
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
    historyItems: history.data?.items ?? [],
    filter,
    setFilter,
    onHistoryTap,
    onResultTap,
    impression,
    onRetry,
    onRefresh: () => { void refetch(); },
    isRefreshing: isSearching && searchData !== undefined,
    correctedQuery: searchData?.corrected_query,
    originalQuery: searchData?.original_query,
    onSearchOriginal,
    onClearHistory,
  };
}

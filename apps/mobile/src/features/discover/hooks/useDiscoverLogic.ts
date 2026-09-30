import { useEffect, type Dispatch, type SetStateAction } from 'react';
import { useQueryClient } from '@tanstack/react-query';

import { discoveryKeys } from '@shared/lib/query-keys';
import { setSearchState } from '../searchState';
import { useDebouncedSearch } from './useDebouncedSearch';
import { useDiscoverSearch } from './useDiscoverSearch';
import { isSearchableQuery } from '../searchLimits';
import { SEARCH_DEBOUNCE_MS, useAutocompleteSuggestions } from './useAutocompleteSuggestions';
import { useImpressionLogger, type ImpressionHandlers } from './useImpressionLogger';
import { useSearchHistory } from './useSearchHistory';
import { useResultsFilter } from './useResultsFilter';
import { useClearSearchHistory } from './useClearSearchHistory';
import { useResultTap } from './useResultTap';
import { useSuggestionVisibility } from './useSuggestionVisibility';
import { useDegradedSearchTelemetry } from './useDegradedSearchTelemetry';
import { correctionForResponse, resultsIncompleteForState, viewForState } from '../discoverView';
import type {
  DiscoveryResult,
  DiscoverySearchResponse,
  DiscoverySuggestion,
  SearchHistoryItem,
} from '@shared/api-client/discovery';
import type { DiscoverView, SearchCorrection } from '../discoverView';
import type { ResultsFilter } from './useResultsFilter';

export type DiscoverLogic = {
  inputValue: string;
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
  resultsIncomplete: boolean;
  searchData: DiscoverySearchResponse | undefined;
  historyItems: SearchHistoryItem[];
  filter: ResultsFilter;
  setFilter: Dispatch<SetStateAction<ResultsFilter>>;
  onHistoryTap: (item: SearchHistoryItem) => void;
  onResultTap: (result: DiscoveryResult, position: number) => void;
  impression: ImpressionHandlers;
  onRetry: () => void;
  searchError: unknown;
  onEndReached: () => void;
  isFetchingNextPage: boolean;
  onRefresh: () => void;
  isRefreshing: boolean;
  correction: SearchCorrection | null;
  onSearchOriginal: () => void;
  onClearHistory: () => void;
  nextPageFailed: boolean;
  onRetryNextPage: () => void;
  clearHistoryFailed: boolean;
  historyFailed: boolean;
  onRetryHistory: () => void;
  refreshFailed: boolean;
};

export function useDiscoverLogic(): DiscoverLogic {
  const search = useDebouncedSearch({ debounceMs: SEARCH_DEBOUNCE_MS });
  const queryClient = useQueryClient();
  const shouldSaveHistory = search.isExplicitSubmit;
  const {
    data: searchData,
    isLoading: isSearching,
    isRefreshing,
    error: searchError,
    isUnavailable,
    refetch,
    fetchNextPage,
    hasNextPage,
    isFetchingNextPage,
    isFetchNextPageError,
    refreshFailed,
  } = useDiscoverSearch(search.committedQuery, shouldSaveHistory);
  const suggestions = useAutocompleteSuggestions(search.inputValue);
  const suggestionItems = suggestions.data?.suggestions ?? [];
  const history = useSearchHistory();
  const impression = useImpressionLogger(searchData);
  const suggestionVisibility = useSuggestionVisibility(search, suggestionItems.length);
  const { filter, setFilter } = useResultsFilter(search.committedQuery);
  const clearHistory = useClearSearchHistory();
  const onResultTap = useResultTap(searchData);
  const hookState = {
    query: search.committedQuery,
    isLoading: isSearching,
    data: searchData,
    error: searchError,
    isUnavailable,
  };
  const resultsIncomplete = resultsIncompleteForState(hookState);
  const correction = correctionForResponse(searchData);
  const trimmedInput = search.inputValue.trim();
  const isSearchPending = isSearchableQuery(trimmedInput) && trimmedInput !== search.committedQuery;
  useDegradedSearchTelemetry(searchData, resultsIncomplete);

  useEffect(() => {
    setSearchState(search.committedQuery, search.inputValue);
  }, [search.committedQuery, search.inputValue]);

  const searchId =
    searchData === undefined ? undefined : (searchData.search_id ?? searchData.query);
  useEffect(() => {
    if (searchId !== undefined) {
      void queryClient.invalidateQueries({ queryKey: discoveryKeys.history });
    }
  }, [searchId, queryClient]);

  const onRetry = (): void => {
    void refetch();
  };

  return {
    inputValue: search.inputValue,
    pending: isSearchPending,
    onChangeText: suggestionVisibility.onChangeText,
    onSubmit: suggestionVisibility.onSubmit,
    onClear: search.onClear,
    isFocused: suggestionVisibility.isFocused,
    setIsFocused: suggestionVisibility.setIsFocused,
    showSuggestions: suggestionVisibility.showSuggestions,
    suggestionItems,
    onSuggestionSelect: suggestionVisibility.onSuggestionSelect,
    view: viewForState(hookState),
    resultsIncomplete,
    searchData,
    historyItems: history.data?.items ?? [],
    filter,
    setFilter,
    onHistoryTap: (item: SearchHistoryItem) => {
      search.setQuery(item.query);
    },
    onResultTap,
    impression,
    onRetry,
    searchError,
    onEndReached: () => {
      if (hasNextPage && !isFetchingNextPage && !isFetchNextPageError && !isRefreshing) {
        void fetchNextPage({ cancelRefetch: false });
      }
    },
    isFetchingNextPage,
    onRefresh: onRetry,
    isRefreshing,
    correction,
    onSearchOriginal: () => {
      if (correction != null) search.setQuery(correction.original);
    },
    onClearHistory: clearHistory.clear,
    nextPageFailed: isFetchNextPageError,
    onRetryNextPage: () => {
      if (!isFetchingNextPage && !isRefreshing) void fetchNextPage({ cancelRefetch: false });
    },
    clearHistoryFailed: clearHistory.error !== null,
    historyFailed: history.error !== null && history.data === undefined,
    onRetryHistory: () => {
      void history.refetch();
    },
    refreshFailed,
  };
}

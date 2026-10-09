import { useEffect, type Dispatch, type SetStateAction } from 'react';
import { useQueryClient } from '@tanstack/react-query';

import { discoveryKeys } from '@shared/query/queryKeys';
import { useDebouncedSearch } from '@features/discover/hooks/useDebouncedSearch';
import { useDiscoverSearch } from '@features/discover/hooks/useDiscoverSearch';
import { isSearchableQuery } from '@features/discover/domain/searchLimits';
import {
  SEARCH_DEBOUNCE_MS,
  useAutocompleteSuggestions,
} from '@features/discover/hooks/useAutocompleteSuggestions';
import {
  useImpressionLogger,
  type ImpressionHandlers,
} from '@features/discover/hooks/useImpressionLogger';
import { useSearchHistory } from '@features/discover/hooks/useSearchHistory';
import { useResultsFilter } from '@features/discover/hooks/useResultsFilter';
import { useDiscoverClearSearchHistory } from '@features/discover/hooks/useDiscoverClearSearchHistory';
import { useResultTap, type ResultTapHandler } from '@features/discover/hooks/useResultTap';
import { useSuggestionVisibility } from '@features/discover/hooks/useSuggestionVisibility';
import { useDegradedSearchTelemetry } from '@features/discover/hooks/useDegradedSearchTelemetry';
import {
  correctionForResponse,
  resultsIncompleteForState,
  viewForState,
} from '@features/discover/domain/discoverView';
import type {
  DiscoverySearchResponse,
  DiscoverySuggestion,
  SearchHistoryItem,
} from '@shared/api-client/discovery';
import type { DiscoverView, SearchCorrection } from '@features/discover/domain/discoverView';
import type { ResultsFilter } from '@features/discover/hooks/useResultsFilter';

export type ResultsSlice = {
  view: DiscoverView;
  searchData: DiscoverySearchResponse | undefined;
  resultsIncomplete: boolean;
  searchError: unknown;
  onRetry: () => void;
  filter: ResultsFilter;
  setFilter: Dispatch<SetStateAction<ResultsFilter>>;
  onResultTap: ResultTapHandler;
  impression: ImpressionHandlers;
  correction: SearchCorrection | null;
  onSearchOriginal: () => void;
  onRefresh: () => void;
  isRefreshing: boolean;
  refreshFailed: boolean;
};

export type PaginationSlice = {
  onEndReached: () => void;
  isFetchingNextPage: boolean;
  nextPageFailed: boolean;
  onRetryNextPage: () => void;
};

export type HistorySlice = {
  items: SearchHistoryItem[];
  failed: boolean;
  clearFailed: boolean;
  onTap: (item: SearchHistoryItem) => void;
  onRetry: () => void;
  onClear: () => void;
};

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
  results: ResultsSlice;
  pagination: PaginationSlice;
  history: HistorySlice;
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
  const clearHistory = useDiscoverClearSearchHistory();
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
    results: {
      view: viewForState(hookState),
      searchData,
      resultsIncomplete,
      searchError,
      onRetry,
      filter,
      setFilter,
      onResultTap,
      impression,
      correction,
      onSearchOriginal: () => {
        if (correction != null) search.setQuery(correction.original);
      },
      onRefresh: onRetry,
      isRefreshing,
      refreshFailed,
    },
    pagination: {
      onEndReached: () => {
        if (hasNextPage && !isFetchingNextPage && !isFetchNextPageError && !isRefreshing) {
          void fetchNextPage({ cancelRefetch: false });
        }
      },
      isFetchingNextPage,
      nextPageFailed: isFetchNextPageError,
      onRetryNextPage: () => {
        if (!isFetchingNextPage && !isRefreshing) void fetchNextPage({ cancelRefetch: false });
      },
    },
    history: {
      items: history.data?.items ?? [],
      failed: history.error !== null && history.data === undefined,
      clearFailed: clearHistory.error !== null,
      onTap: (item: SearchHistoryItem) => {
        search.setQuery(item.query);
      },
      onRetry: () => {
        void history.refetch();
      },
      onClear: clearHistory.clear,
    },
  };
}

import { type Dispatch, type SetStateAction } from 'react';

import { useDebouncedSearch } from '@features/discovery/hooks/useDebouncedSearch';
import { useDiscoverSearch } from '@features/discovery/hooks/useDiscoverSearch';
import { isSearchableQuery, normalizeQuery } from '@features/discovery/domain/searchPaging';
import {
  SEARCH_DEBOUNCE_MS,
  useAutocompleteSuggestions,
  useSuggestionVisibility,
  visibleSuggestions,
} from '@features/discovery/hooks/useSuggestions';
import {
  useImpressionLogger,
  type ImpressionHandlers,
} from '@features/discovery/hooks/useImpressionLogger';
import {
  useDiscoverClearSearchHistory,
  useRecordSearchHistory,
  useSearchHistory,
} from '@features/discovery/hooks/useSearchHistory';
import { useResultsFilter } from '@features/discovery/hooks/useResultsFilter';
import { useResultTap, type ResultTapHandler } from '@features/discovery/hooks/useResultTap';
import { useDegradedSearchTelemetry } from '@features/discovery/hooks/useDegradedSearchTelemetry';
import {
  correctionForResponse,
  resultsIncompleteForState,
  viewForState,
} from '@features/discovery/domain/discoverView';
import type {
  DiscoverySearchResponse,
  DiscoverySuggestion,
  SearchHistoryItem,
} from '@shared/apiClient/discovery';
import type { DiscoverView, SearchCorrection } from '@features/discovery/domain/discoverView';
import type { ResultsFilter } from '@features/discovery/hooks/useResultsFilter';

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
  const recordSearch = useRecordSearchHistory();
  const search = useDebouncedSearch({
    debounceMs: SEARCH_DEBOUNCE_MS,
    onExplicitCommit: recordSearch,
  });
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
    isShowingPrevious,
  } = useDiscoverSearch(search.committedQuery);
  const suggestionVisibility = useSuggestionVisibility(search);
  const suggestions = useAutocompleteSuggestions(
    search.inputValue,
    suggestionVisibility.canShowSuggestions,
  );
  const suggestionItems = visibleSuggestions(
    suggestionVisibility.canShowSuggestions,
    suggestions.data,
  );
  const history = useSearchHistory();
  const impression = useImpressionLogger(searchData);
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
  const trimmedInput = normalizeQuery(search.inputValue);
  const isSearchPending =
    (isSearchableQuery(trimmedInput) && trimmedInput !== search.committedQuery) ||
    isShowingPrevious;
  useDegradedSearchTelemetry(searchData, resultsIncomplete);

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
    showSuggestions: suggestionItems.length > 0,
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
        suggestionVisibility.onSuggestionSelect(item.query);
      },
      onRetry: () => {
        void history.refetch();
      },
      onClear: clearHistory.clear,
    },
  };
}

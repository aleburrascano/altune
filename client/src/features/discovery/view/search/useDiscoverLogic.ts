import { type Dispatch, type SetStateAction } from 'react';

import { useDebouncedSearch } from '@features/discovery/view/search/useDebouncedSearch';
import { useDiscoverSearch } from '@features/discovery/view/search/useDiscoverSearch';
import { useUnreleasedSearch } from '@features/discovery/view/unreleased/useUnreleasedSearch';
import { isSearchableQuery, normalizeQuery } from '@features/discovery/core/searchPaging';
import {
  SEARCH_DEBOUNCE_MS,
  useAutocompleteSuggestions,
  useSuggestionVisibility,
  visibleSuggestions,
} from '@features/discovery/view/history/useSuggestions';
import {
  useImpressionLogger,
  type ImpressionHandlers,
} from '@features/discovery/view/results/useImpressionLogger';
import {
  useDiscoverClearSearchHistory,
  useRecordSearchHistory,
  useSearchHistory,
} from '@features/discovery/view/history/useSearchHistory';
import { useResultsFilter } from '@features/discovery/view/results/useResultsFilter';
import type { DiscoverEnv } from '@features/discovery/view/search/screenEnv';
import { useResultTap, type ResultTapHandler } from '@features/discovery/view/results/useResultTap';
import { useDegradedSearchTelemetry } from '@features/discovery/view/search/useDegradedSearchTelemetry';
import {
  correctionForResponse,
  resultsIncompleteForState,
  viewForState,
} from '@features/discovery/core/discoverView';
import type { UnreleasedSearchHit } from '@shared/apiClient';
import type {
  DiscoverySearchResponse,
  DiscoverySuggestion,
  SearchHistoryItem,
} from '@shared/catalog';
import type { DiscoverView, SearchCorrection } from '@features/discovery/core/discoverView';
import type { ResultsFilter } from '@features/discovery/view/results/useResultsFilter';

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
  unreleased: UnreleasedSearchHit[];
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

type HistoryParts = {
  history: ReturnType<typeof useSearchHistory>;
  clearHistory: ReturnType<typeof useDiscoverClearSearchHistory>;
  onSuggestionSelect: (text: string) => void;
};

type HistoryStatus = Pick<HistorySlice, 'items' | 'failed' | 'clearFailed'>;
type HistoryActions = Pick<HistorySlice, 'onTap' | 'onRetry' | 'onClear'>;

function historyStatus({ history, clearHistory }: HistoryParts): HistoryStatus {
  return {
    items: history.data?.items ?? [],
    failed: history.error !== null && history.data === undefined,
    clearFailed: clearHistory.error !== null,
  };
}

function historyActions(parts: HistoryParts): HistoryActions {
  return {
    onTap: (item: SearchHistoryItem): void => parts.onSuggestionSelect(item.query),
    onRetry: (): void => parts.history.refetch(),
    onClear: parts.clearHistory.clear,
  };
}

function historySlice(parts: HistoryParts): HistorySlice {
  return { ...historyStatus(parts), ...historyActions(parts) };
}

export function useDiscoverLogic(env: DiscoverEnv): DiscoverLogic {
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
  const unreleased = useUnreleasedSearch(search.committedQuery);
  const history = useSearchHistory();
  const impression = useImpressionLogger(searchData);
  const { filter, setFilter } = useResultsFilter(search.committedQuery);
  const clearHistory = useDiscoverClearSearchHistory();
  const onResultTap = useResultTap(searchData, env);
  const hookState = {
    query: search.committedQuery,
    isLoading: isSearching,
    data: searchData,
    error: searchError,
  };
  const resultsIncomplete = resultsIncompleteForState(hookState);
  const correction = correctionForResponse(searchData);
  const trimmedInput = normalizeQuery(search.inputValue);
  const isSearchPending =
    (isSearchableQuery(trimmedInput) && trimmedInput !== search.committedQuery) ||
    isShowingPrevious;
  useDegradedSearchTelemetry(searchData, resultsIncomplete);

  const onRetry = (): void => {
    refetch();
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
      unreleased,
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
    history: historySlice({
      history,
      clearHistory,
      onSuggestionSelect: suggestionVisibility.onSuggestionSelect,
    }),
  };
}

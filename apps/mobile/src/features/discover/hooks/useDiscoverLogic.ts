import { useEffect, type Dispatch, type SetStateAction } from 'react';
import { useQueryClient } from '@tanstack/react-query';

import { discoveryKeys } from '@shared/lib/query-keys';
import { setSearchState } from '../search-state';
import { useDebouncedSearch } from './useDebouncedSearch';
import { useDiscoverSearch } from './useDiscoverSearch';
import { useAutocompleteSuggestions } from './useAutocompleteSuggestions';
import { useImpressionLogger, type ImpressionHandlers } from './useImpressionLogger';
import { useSearchHistory } from './useSearchHistory';
import { useResultsFilter } from './useResultsFilter';
import { useClearSearchHistory } from './useClearSearchHistory';
import { useResultTap } from './useResultTap';
import { useSuggestionVisibility } from './useSuggestionVisibility';
import { _viewForState } from '../state';
import type {
  DiscoveryResult,
  DiscoverySearchResponse,
  DiscoverySuggestion,
  SearchHistoryItem,
} from '@shared/api-client/discovery';
import type { DiscoverView } from '../state';
import type { ResultsFilter } from './useResultsFilter';

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
  /** Set when the suggest query failed, so a broken endpoint is distinguishable from no suggestions. */
  suggestionsError: Error | null;
  onSuggestionSelect: (text: string) => void;
  view: DiscoverView;
  searchData: DiscoverySearchResponse | undefined;
  historyItems: SearchHistoryItem[];
  /** Set when the history query failed, so a broken endpoint is distinguishable from empty history. */
  historyError: Error | null;
  filter: ResultsFilter;
  setFilter: Dispatch<SetStateAction<ResultsFilter>>;
  onHistoryTap: (item: SearchHistoryItem) => void;
  onResultTap: (result: DiscoveryResult, position: number) => void;
  impression: ImpressionHandlers;
  onRetry: () => void;
  searchError: unknown;
  onEndReached: () => void;
  hasNextPage: boolean;
  isFetchingNextPage: boolean;
  onRefresh: () => void;
  isRefreshing: boolean;
  correctedQuery: string | undefined;
  originalQuery: string | undefined;
  onSearchOriginal: () => void;
  onClearHistory: () => void;
};

export function useDiscoverLogic(): DiscoverLogic {
  const search = useDebouncedSearch({ debounceMs: 300, minChars: 2 });
  const queryClient = useQueryClient();
  const {
    data: searchData,
    isLoading: isSearching,
    error: searchError,
    refetch,
    fetchNextPage,
    hasNextPage,
    isFetchingNextPage,
  } = useDiscoverSearch(search.committedQuery, search.isExplicitSubmit);
  const suggestions = useAutocompleteSuggestions(search.inputValue);
  const suggestionItems = suggestions.data?.suggestions ?? [];
  const history = useSearchHistory();
  const impression = useImpressionLogger(searchData);
  const suggestionVisibility = useSuggestionVisibility(search, suggestionItems.length);
  const { filter, setFilter } = useResultsFilter(search.committedQuery);
  const onClearHistory = useClearSearchHistory();
  const onResultTap = useResultTap(searchData, search.committedQuery);

  useEffect(() => {
    setSearchState(search.committedQuery, search.inputValue);
  }, [search.committedQuery, search.inputValue]);

  useEffect(() => {
    if (searchData) {
      void queryClient.invalidateQueries({ queryKey: discoveryKeys.history });
    }
  }, [searchData, queryClient]);

  const onRetry = (): void => {
    void refetch();
  };

  return {
    inputValue: search.inputValue,
    committedQuery: search.committedQuery,
    pending:
      search.inputValue.trim().length >= 2 && search.inputValue.trim() !== search.committedQuery,
    onChangeText: suggestionVisibility.onChangeText,
    onSubmit: suggestionVisibility.onSubmit,
    onClear: search.onClear,
    isFocused: suggestionVisibility.isFocused,
    setIsFocused: suggestionVisibility.setIsFocused,
    showSuggestions: suggestionVisibility.showSuggestions,
    suggestionItems,
    suggestionsError: suggestions.error,
    onSuggestionSelect: suggestionVisibility.onSuggestionSelect,
    view: _viewForState({
      query: search.committedQuery,
      isLoading: isSearching,
      data: searchData,
      error: searchError,
    }),
    searchData,
    historyItems: history.data?.items ?? [],
    historyError: history.error,
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
      if (hasNextPage && !isFetchingNextPage) void fetchNextPage();
    },
    hasNextPage: hasNextPage ?? false,
    isFetchingNextPage,
    onRefresh: onRetry,
    isRefreshing: isSearching && searchData !== undefined,
    correctedQuery: searchData?.corrected_query,
    originalQuery: searchData?.original_query,
    onSearchOriginal: () => {
      if (searchData?.original_query) search.setQuery(searchData.original_query);
    },
    onClearHistory,
  };
}

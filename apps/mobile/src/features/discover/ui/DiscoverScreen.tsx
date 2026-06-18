/**
 * DiscoverScreen — Spotify-style sectioned multi-kind search (discover-music-v2).
 *
 * Filter chips (All · Albums · Songs · Artists) sit atop the results. "All" is a
 * blended view: a prominent Top Result card, then capped Albums / Songs / Artists
 * sections with "See all" affordances. A kind chip filters to a flat list of that
 * kind. Confidence is no longer displayed anywhere.
 *
 * TestIDs preserved + extended: discover-search-input, discover-loading,
 * discover-empty-no-query (+ discover-history-row-<i>), discover-results,
 * discover-partial-banner, discover-zero-results, discover-full-error
 * (+ discover-retry), discover-row-<kind>-<position>, discover-filter-<all|album|track|artist>,
 * discover-top-result.
 */

import { useRouter } from 'expo-router';
import type { ReactElement } from 'react';
import { useEffect, useRef, useState } from 'react';
import { Keyboard, Pressable, StyleSheet, View } from 'react-native';
import { useQueryClient } from '@tanstack/react-query';

import { Screen, Text, spacing, useTheme } from '@shared/ui';
import { setSearchState } from '@shared/lib/search-state';

import { SearchBar } from './SearchBar';
import { DiscoverBody } from './DiscoverBody';
import { SuggestionsList } from './SuggestionsList';
import { useDebouncedSearch } from '../hooks/useDebouncedSearch';
import { useDiscoverSearch } from '../hooks/useDiscoverSearch';
import { useAutocompleteSuggestions } from '../hooks/useAutocompleteSuggestions';
import { useRecordClick } from '../hooks/useRecordClick';
import { useSearchHistory } from '../hooks/useSearchHistory';
import { stashHandoffForDetail } from '../tap';
import { _viewForState } from '../state';
import type {
  DiscoveryKind,
  DiscoveryResult,
  SearchHistoryItem,
} from '../../../shared/api-client/discovery';

type ResultsFilter = 'all' | DiscoveryKind;

export function DiscoverScreen(): ReactElement {
  const theme = useTheme();
  const router = useRouter();
  const search = useDebouncedSearch({ debounceMs: 300, minChars: 2 });
  const [filter, setFilter] = useState<ResultsFilter>('all');
  const queryClient = useQueryClient();
  const { data: searchData, isLoading: isSearching, error: searchError, refetch } = useDiscoverSearch(search.committedQuery, search.isExplicitSubmit);
  const suggestions = useAutocompleteSuggestions(search.inputValue);
  const history = useSearchHistory();
  const click = useRecordClick();
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
    click.mutate({
      query_norm: searchData?.query_norm ?? search.committedQuery,
      kind: result.kind,
      title: result.title,
      subtitle: result.subtitle ?? null,
      position,
      confidence: result.confidence,
    });
    router.push(stashHandoffForDetail(result));
  };
  const onChangeText = (text: string): void => {
    setSuggestionsHidden(false);
    search.onChangeText(text);
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

  return (
    <Screen>
      <Pressable onPress={Keyboard.dismiss} style={styles.flex}>
      <View style={styles.titleBlock}>
        <Text variant="displayL" style={styles.title}>
          Discover
        </Text>
      </View>
      <SearchBar
        value={search.inputValue}
        onChangeText={onChangeText}
        onSubmitEditing={() => { setSuggestionsHidden(true); search.onSubmit(); }}
        onClear={search.onClear}
        onFocus={() => setIsFocused(true)}
        onBlur={() => setIsFocused(false)}
        focused={isFocused}
        pending={search.inputValue.trim().length >= 2 && search.inputValue.trim() !== search.committedQuery}
        suggestionsOpen={showSuggestions}
        theme={theme}
      >
        {showSuggestions && (
          <SuggestionsList
            suggestions={suggestions.data!.suggestions}
            onSelect={onSuggestionSelect}
          />
        )}
      </SearchBar>
      <DiscoverBody
        view={view}
        searchData={searchData}
        history={history}
        filter={filter}
        onFilterChange={setFilter}
        onHistoryTap={onHistoryTap}
        onResultTap={onResultTap}
        onRetry={onRetry}
        onRefresh={() => { void refetch(); }}
        isRefreshing={isSearching && searchData !== undefined}
        correctedQuery={searchData?.corrected_query}
        originalQuery={searchData?.original_query}
        onSearchOriginal={onSearchOriginal}
        onClearHistory={onClearHistory}
      />
      </Pressable>
    </Screen>
  );
}

const styles = StyleSheet.create({
  flex: { flex: 1 },
  titleBlock: { paddingTop: spacing.sm },
  title: { marginTop: spacing.xs },
});

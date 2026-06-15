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
import { useDebouncedSearch } from '../hooks/useDebouncedSearch';
import { useDiscoverSearch } from '../hooks/useDiscoverSearch';
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
  const { data: searchData, isLoading: isSearching, error: searchError } = useDiscoverSearch(search.committedQuery);
  const history = useSearchHistory();
  const click = useRecordClick();

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
  const onResultTap = (result: DiscoveryResult, position: number): void => {
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
  const onRetry = (): void => {
    search.setQuery(search.committedQuery.trim() || search.committedQuery);
  };

  return (
    <Screen>
      <Pressable onPress={Keyboard.dismiss} style={styles.flex}>
      <View style={styles.titleBlock}>
        <Text variant="label" tone="tertiary">
          {_greeting()}
        </Text>
        <Text variant="displayL" style={styles.title}>
          Discover
        </Text>
      </View>
      <SearchBar
        value={search.inputValue}
        onChangeText={search.onChangeText}
        onSubmitEditing={search.onSubmit}
        onClear={search.onClear}
        theme={theme}
      />
      <DiscoverBody
        view={view}
        searchData={searchData}
        history={history}
        filter={filter}
        onFilterChange={setFilter}
        onHistoryTap={onHistoryTap}
        onResultTap={onResultTap}
        onRetry={onRetry}
      />
      </Pressable>
    </Screen>
  );
}

/** Time-of-day greeting above the Discover title. */
function _greeting(): string {
  const hour = new Date().getHours();
  if (hour < 12) {
    return 'Good morning';
  }
  if (hour < 18) {
    return 'Good afternoon';
  }
  return 'Good evening';
}

const styles = StyleSheet.create({
  flex: { flex: 1 },
  titleBlock: { paddingTop: spacing.sm },
  title: { marginTop: spacing.xs },
});

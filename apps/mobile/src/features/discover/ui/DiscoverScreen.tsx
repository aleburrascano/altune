/**
 * DiscoverScreen — paginated multi-source search with designed states.
 *
 * Slice 46. State machine in ../state.ts; testIDs per AC#20:
 *   discover-loading, discover-empty-no-query (+ discover-history-row-<i>),
 *   discover-results, discover-zero-results,
 *   discover-full-error (+ discover-retry), discover-partial-banner.
 */

import type { ReactElement } from 'react';
import { useState } from 'react';
import {
  ActivityIndicator,
  FlatList,
  StyleSheet,
  Text,
  TextInput,
  TouchableOpacity,
  View,
  type ListRenderItem,
} from 'react-native';

import { DiscoverRow } from './DiscoverRow';
import { PartialBanner } from './PartialBanner';
import { useDiscoverSearch } from '../hooks/useDiscoverSearch';
import { useRecordClick } from '../hooks/useRecordClick';
import { useSearchHistory } from '../hooks/useSearchHistory';
import { _shouldShowPartialBanner, _viewForState } from '../state';

import type {
  DiscoveryResult,
  SearchHistoryItem,
} from '../../../shared/api-client/discovery';

const _renderResult =
  (
    onTap: (result: DiscoveryResult, position: number) => void,
  ): ListRenderItem<DiscoveryResult> =>
  ({ item, index }) =>
    <DiscoverRow result={item} position={index} onPress={onTap} />;

const _historyKey = (item: SearchHistoryItem): string => item.query_norm;

export function DiscoverScreen(): ReactElement {
  const [committedQuery, setCommittedQuery] = useState('');
  const [inputValue, setInputValue] = useState('');
  const search = useDiscoverSearch(committedQuery);
  const history = useSearchHistory();
  const click = useRecordClick();

  const view = _viewForState({
    query: committedQuery,
    isLoading: search.isLoading,
    data: search.data,
    error: search.error as Error | null,
  });

  const onSubmit = (): void => setCommittedQuery(inputValue.trim());
  const onHistoryTap = (item: SearchHistoryItem): void => {
    setInputValue(item.query);
    setCommittedQuery(item.query);
  };
  const onResultTap = (result: DiscoveryResult, position: number): void => {
    click.mutate({
      query_norm: search.data?.query_norm ?? committedQuery,
      kind: result.kind,
      title: result.title,
      subtitle: result.subtitle ?? null,
      position,
      confidence: result.confidence,
    });
  };

  let body: ReactElement;
  if (view === 'loading') {
    body = (
      <View style={styles.center} testID="discover-loading">
        <ActivityIndicator color="#fff" />
      </View>
    );
  } else if (view === 'full-error') {
    body = (
      <View style={styles.center} testID="discover-full-error">
        <Text style={styles.errorText}>Search failed.</Text>
        <TouchableOpacity
          testID="discover-retry"
          style={styles.retryButton}
          onPress={() => setCommittedQuery((q) => (q ? `${q} ` : q).trim() || q)}
        >
          <Text style={styles.retryText}>Retry</Text>
        </TouchableOpacity>
      </View>
    );
  } else if (view === 'zero-results') {
    body = (
      <View style={styles.center} testID="discover-zero-results">
        <Text style={styles.emptyText}>No matches.</Text>
      </View>
    );
  } else if (view === 'empty-no-query') {
    body = (
      <View testID="discover-empty-no-query" style={styles.history}>
        <Text style={styles.sectionHeader}>Recent searches</Text>
        <FlatList
          data={history.data?.items ?? []}
          keyExtractor={_historyKey}
          renderItem={({ item, index }) => (
            <TouchableOpacity
              testID={`discover-history-row-${index}`}
              style={styles.historyRow}
              onPress={() => onHistoryTap(item)}
            >
              <Text style={styles.historyText} numberOfLines={1}>
                {item.query.length > 40 ? `${item.query.slice(0, 40)}…` : item.query}
              </Text>
            </TouchableOpacity>
          )}
          ListEmptyComponent={
            <Text style={styles.historyEmpty}>Search music to get started.</Text>
          }
        />
      </View>
    );
  } else {
    body = (
      <View testID="discover-results" style={styles.resultsContainer}>
        {_shouldShowPartialBanner(search.data) && search.data && (
          <PartialBanner providers={search.data.providers} />
        )}
        <FlatList
          data={search.data?.results ?? []}
          keyExtractor={(r) => `${r.kind}-${r.title}-${r.subtitle ?? ''}`}
          renderItem={_renderResult(onResultTap)}
        />
      </View>
    );
  }

  return (
    <View style={styles.screen}>
      <View style={styles.header}>
        <TextInput
          style={styles.input}
          placeholder="Search music"
          placeholderTextColor="#666"
          value={inputValue}
          onChangeText={setInputValue}
          onSubmitEditing={onSubmit}
          returnKeyType="search"
          testID="discover-search-input"
          autoCapitalize="none"
          autoCorrect={false}
        />
      </View>
      {body}
    </View>
  );
}

const styles = StyleSheet.create({
  screen: { flex: 1, backgroundColor: '#000' },
  header: {
    paddingHorizontal: 16,
    paddingTop: 48,
    paddingBottom: 8,
    borderBottomWidth: StyleSheet.hairlineWidth,
    borderBottomColor: '#222',
  },
  input: {
    backgroundColor: '#1f1f1f',
    color: '#fff',
    paddingHorizontal: 12,
    paddingVertical: 10,
    borderRadius: 8,
    fontSize: 15,
  },
  center: {
    flex: 1,
    alignItems: 'center',
    justifyContent: 'center',
    padding: 24,
  },
  errorText: { color: '#fff', fontSize: 16, marginBottom: 16 },
  retryButton: {
    paddingVertical: 10,
    paddingHorizontal: 24,
    borderRadius: 8,
    backgroundColor: '#1f1f1f',
  },
  retryText: { color: '#fff', fontSize: 14, fontWeight: '500' },
  emptyText: { color: '#fff', fontSize: 18, fontWeight: '500' },
  history: { flex: 1, paddingTop: 16 },
  sectionHeader: {
    color: '#888',
    fontSize: 12,
    textTransform: 'uppercase',
    paddingHorizontal: 16,
    marginBottom: 8,
  },
  historyRow: {
    paddingHorizontal: 16,
    paddingVertical: 10,
    borderBottomWidth: StyleSheet.hairlineWidth,
    borderBottomColor: '#222',
  },
  historyText: { color: '#fff', fontSize: 14 },
  historyEmpty: { color: '#888', fontSize: 14, padding: 16 },
  resultsContainer: { flex: 1 },
});

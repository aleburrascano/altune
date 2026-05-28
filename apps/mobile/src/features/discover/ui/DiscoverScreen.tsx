/**
 * DiscoverScreen — paginated multi-source search with designed states (ADR-0008).
 *
 * Slice 46. State machine in ../state.ts (untouched); testIDs per AC#20:
 *   discover-search-input, discover-loading, discover-empty-no-query
 *   (+ discover-history-row-<i>), discover-results, discover-partial-banner,
 *   discover-zero-results, discover-full-error (+ discover-retry).
 */

import type { ReactElement } from 'react';
import { useState } from 'react';
import { FlatList, StyleSheet, TextInput, View, type ListRenderItem } from 'react-native';

import {
  Button,
  Card,
  Chip,
  Screen,
  Skeleton,
  Text,
  fontFamily,
  radius,
  spacing,
  useTheme,
} from '@shared/ui';

import { DiscoverRow } from './DiscoverRow';
import { PartialBanner } from './PartialBanner';
import { useDiscoverSearch } from '../hooks/useDiscoverSearch';
import { useRecordClick } from '../hooks/useRecordClick';
import { useSearchHistory } from '../hooks/useSearchHistory';
import { _shouldShowPartialBanner, _viewForState } from '../state';

import type { DiscoveryResult, SearchHistoryItem } from '../../../shared/api-client/discovery';

const _renderResult =
  (onTap: (result: DiscoveryResult, position: number) => void): ListRenderItem<DiscoveryResult> =>
  ({ item, index }) => <DiscoverRow result={item} position={index} onPress={onTap} />;

const SKELETON_ROWS = [0, 1, 2, 3, 4, 5];

export function DiscoverScreen(): ReactElement {
  const theme = useTheme();
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
      <View testID="discover-loading" style={styles.list}>
        {SKELETON_ROWS.map((i) => (
          <Card key={i} style={styles.skeletonCard}>
            <View style={styles.skeletonRow}>
              <Skeleton width={52} height={52} radius={radius.md} />
              <View style={styles.skeletonText}>
                <Skeleton width="70%" height={14} />
                <Skeleton width="40%" height={12} />
              </View>
            </View>
          </Card>
        ))}
      </View>
    );
  } else if (view === 'full-error') {
    body = (
      <View testID="discover-full-error" style={styles.center}>
        <Text variant="title">Search failed</Text>
        <Text variant="label" tone="secondary" style={styles.centerSub}>
          Something went wrong. Try again.
        </Text>
        <Button
          testID="discover-retry"
          label="Retry"
          onPress={() => setCommittedQuery((q) => (q ? `${q} ` : q).trim() || q)}
        />
      </View>
    );
  } else if (view === 'zero-results') {
    body = (
      <View testID="discover-zero-results" style={styles.center}>
        <Text variant="title">No matches</Text>
        <Text variant="label" tone="secondary" style={styles.centerSub}>
          Try a different search.
        </Text>
      </View>
    );
  } else if (view === 'empty-no-query') {
    const items = history.data?.items ?? [];
    body = (
      <View testID="discover-empty-no-query" style={styles.list}>
        <Text variant="label" tone="tertiary" style={styles.sectionHeader}>
          RECENT SEARCHES
        </Text>
        {items.length === 0 ? (
          <Text variant="body" tone="secondary">
            Search music to get started.
          </Text>
        ) : (
          <View style={styles.chipCloud}>
            {items.map((item, index) => (
              <Chip
                key={item.query_norm}
                testID={`discover-history-row-${index}`}
                label={item.query.length > 40 ? `${item.query.slice(0, 40)}…` : item.query}
                onPress={() => onHistoryTap(item)}
              />
            ))}
          </View>
        )}
      </View>
    );
  } else {
    body = (
      <View testID="discover-results" style={styles.results}>
        {_shouldShowPartialBanner(search.data) && search.data ? (
          <PartialBanner providers={search.data.providers} />
        ) : null}
        <FlatList
          data={search.data?.results ?? []}
          keyExtractor={(r) => `${r.kind}-${r.title}-${r.subtitle ?? ''}`}
          renderItem={_renderResult(onResultTap)}
          contentContainerStyle={styles.listContent}
          showsVerticalScrollIndicator={false}
        />
      </View>
    );
  }

  return (
    <Screen>
      <View style={styles.header}>
        <TextInput
          style={[
            styles.input,
            { backgroundColor: theme.color.surface1, color: theme.color.textPrimary },
          ]}
          placeholder="Search music"
          placeholderTextColor={theme.color.textTertiary}
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
    </Screen>
  );
}

const styles = StyleSheet.create({
  header: { paddingTop: spacing.sm, paddingBottom: spacing.md },
  input: {
    borderRadius: radius.md,
    paddingHorizontal: spacing.lg,
    paddingVertical: spacing.md,
    fontFamily: fontFamily.bodyRegular,
    fontSize: 16,
  },
  list: { flex: 1, paddingTop: spacing.sm },
  results: { flex: 1 },
  listContent: { paddingTop: spacing.sm, paddingBottom: spacing.xl },
  skeletonCard: { marginBottom: spacing.sm },
  skeletonRow: { flexDirection: 'row', alignItems: 'center', gap: spacing.md },
  skeletonText: { flex: 1, gap: spacing.sm },
  sectionHeader: { marginBottom: spacing.md, letterSpacing: 1 },
  chipCloud: { flexDirection: 'row', flexWrap: 'wrap', gap: spacing.sm },
  center: { flex: 1, alignItems: 'center', justifyContent: 'center', padding: spacing['2xl'] },
  centerSub: { marginTop: spacing.xs, marginBottom: spacing.lg },
});

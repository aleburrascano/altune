import type { ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';

import {
  Button,
  Card,
  Chip,
  Skeleton,
  Text,
  radius,
  spacing,
} from '@shared/ui';

import { BlendedSection } from './BlendedSection';
import { FilteredResults } from './FilteredResults';
import type { DiscoveryKind, DiscoveryResult, SearchHistoryItem } from '../../../shared/api-client/discovery';
import type { DiscoverView } from '../state';

type ResultsFilter = 'all' | DiscoveryKind;

const FILTER_CHIPS: ReadonlyArray<{ filter: ResultsFilter; label: string; testID: string }> = [
  { filter: 'all', label: 'All', testID: 'discover-filter-all' },
  { filter: 'album', label: 'Albums', testID: 'discover-filter-album' },
  { filter: 'track', label: 'Songs', testID: 'discover-filter-track' },
  { filter: 'artist', label: 'Artists', testID: 'discover-filter-artist' },
];

const SKELETON_ROWS = [0, 1, 2, 3, 4, 5];

interface SearchData {
  results: DiscoveryResult[];
  query_norm?: string;
}

interface DiscoverBodyProps {
  view: DiscoverView;
  searchData: SearchData | undefined;
  history: { data?: { items: SearchHistoryItem[] } | undefined };
  filter: ResultsFilter;
  onFilterChange: (filter: ResultsFilter) => void;
  onHistoryTap: (item: SearchHistoryItem) => void;
  onResultTap: (result: DiscoveryResult, position: number) => void;
  onRetry: () => void;
}

export function DiscoverBody({
  view,
  searchData,
  history,
  filter,
  onFilterChange,
  onHistoryTap,
  onResultTap,
  onRetry,
}: DiscoverBodyProps): ReactElement {
  if (view === 'loading') {
    return (
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
  }

  if (view === 'full-error') {
    return (
      <View testID="discover-full-error" style={styles.center}>
        <Text variant="title">Search failed</Text>
        <Text variant="label" tone="secondary" style={styles.centerSub}>
          Something went wrong. Try again.
        </Text>
        <Button testID="discover-retry" label="Retry" onPress={onRetry} />
      </View>
    );
  }

  if (view === 'zero-results') {
    return (
      <View testID="discover-zero-results" style={styles.center}>
        <Text variant="title">No matches</Text>
        <Text variant="label" tone="secondary" style={styles.centerSub}>
          Try a different search.
        </Text>
      </View>
    );
  }

  if (view === 'empty-no-query') {
    const items = history.data?.items ?? [];
    return (
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
  }

  // view === 'results'
  const results = searchData?.results ?? [];
  return (
    <View testID="discover-results" style={styles.results}>
      <FilterChips active={filter} onSelect={onFilterChange} />
      {filter === 'all' ? (
        <BlendedSection
          results={results}
          onResultTap={onResultTap}
          onSeeAll={onFilterChange}
        />
      ) : (
        <FilteredResults kind={filter} results={results} onResultTap={onResultTap} />
      )}
    </View>
  );
}

function FilterChips({
  active,
  onSelect,
}: {
  active: ResultsFilter;
  onSelect: (filter: ResultsFilter) => void;
}): ReactElement {
  return (
    <View style={styles.chipRow}>
      {FILTER_CHIPS.map(({ filter, label, testID }) => (
        <Chip
          key={filter}
          testID={testID}
          label={label}
          selected={active === filter}
          onPress={() => onSelect(filter)}
        />
      ))}
    </View>
  );
}

const styles = StyleSheet.create({
  list: { flex: 1, paddingTop: spacing.sm },
  results: { flex: 1 },
  skeletonCard: { marginBottom: spacing.sm },
  skeletonRow: { flexDirection: 'row', alignItems: 'center', gap: spacing.md },
  skeletonText: { flex: 1, gap: spacing.sm },
  sectionHeader: { marginBottom: spacing.md, letterSpacing: 1 },
  chipRow: {
    flexDirection: 'row',
    gap: spacing.sm,
    paddingBottom: spacing.md,
    flexWrap: 'wrap',
  },
  chipCloud: { flexDirection: 'row', flexWrap: 'wrap', gap: spacing.sm },
  center: { flex: 1, alignItems: 'center', justifyContent: 'center', padding: spacing['2xl'] },
  centerSub: { marginTop: spacing.xs, marginBottom: spacing.lg },
});

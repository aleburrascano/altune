import type { ReactElement } from 'react';
import { Pressable, StyleSheet, View } from 'react-native';
import { Search } from 'lucide-react-native';

import {
  Button,
  Chip,
  Skeleton,
  Text,
  radius,
  spacing,
  useTheme,
} from '@shared/ui';

import { BlendedSection } from './BlendedSection';
import { FilteredResults } from './FilteredResults';
import { kindLabel } from '../state';
import type { DiscoveryResult, SearchHistoryItem } from '@shared/api-client/discovery';
import type { DiscoverView, ResultsFilter } from '../state';
import type { ImpressionHandlers } from '../hooks/useImpressionLogger';
import type { ResultsCommonProps } from './ResultsList';

const FILTER_CHIPS: readonly { filter: ResultsFilter; label: string; testID: string }[] = [
  { filter: 'all', label: 'All', testID: 'discover-filter-all' },
  { filter: 'album', label: kindLabel('album', { plural: true }), testID: 'discover-filter-album' },
  { filter: 'track', label: kindLabel('track', { plural: true }), testID: 'discover-filter-track' },
  { filter: 'artist', label: kindLabel('artist', { plural: true }), testID: 'discover-filter-artist' },
];

const SKELETON_ROWS = [0, 1, 2, 3, 4, 5];

interface SearchData {
  results: DiscoveryResult[];
  query_norm?: string;
}

interface DiscoverBodyProps {
  view: DiscoverView;
  searchData: SearchData | undefined;
  historyItems: SearchHistoryItem[];
  filter: ResultsFilter;
  onFilterChange: (filter: ResultsFilter) => void;
  onHistoryTap: (item: SearchHistoryItem) => void;
  onResultTap: (result: DiscoveryResult, position: number) => void;
  impression: ImpressionHandlers;
  onRetry: () => void;
  onRefresh: () => void;
  isRefreshing: boolean;
  correctedQuery?: string | undefined;
  originalQuery?: string | undefined;
  onSearchOriginal: () => void;
  onClearHistory?: (() => void) | undefined;
}

export function DiscoverBody({
  view,
  searchData,
  historyItems,
  filter,
  onFilterChange,
  onHistoryTap,
  onResultTap,
  impression,
  onRetry,
  onRefresh,
  isRefreshing,
  correctedQuery,
  originalQuery,
  onSearchOriginal,
  onClearHistory,
}: DiscoverBodyProps): ReactElement {
  const theme = useTheme();

  if (view === 'loading') {
    return (
      <View testID="discover-loading" style={styles.list}>
        {SKELETON_ROWS.map((i) => (
          <View key={i} style={styles.skeletonRow}>
            <Skeleton width={56} height={56} radius={radius.md} />
            <View style={styles.skeletonText}>
              <Skeleton width="70%" height={14} />
              <Skeleton width="40%" height={12} />
            </View>
          </View>
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
      <View testID="discover-zero-results" style={styles.zeroResults}>
        <FilterChips active={filter} onSelect={onFilterChange} />
        <View style={styles.center}>
          <Text variant="title">No matches</Text>
          <Text variant="label" tone="secondary" style={styles.centerSub}>
            Check spelling or try fewer words.
          </Text>
        </View>
      </View>
    );
  }

  if (view === 'empty-no-query') {
    return (
      <View testID="discover-empty-no-query" style={styles.list}>
        {historyItems.length === 0 ? (
          <View style={styles.emptyCenter}>
            <Search size={32} color={theme.color.textTertiary} />
            <Text variant="body" tone="secondary" style={styles.emptyText}>
              Search music to get started.
            </Text>
          </View>
        ) : (
          <>
          <View style={styles.historyHeader}>
            <Text variant="label" tone="tertiary" style={styles.sectionHeader}>
              RECENT SEARCHES
            </Text>
            {onClearHistory != null ? (
              <Pressable
                onPress={onClearHistory}
                accessibilityRole="button"
                accessibilityLabel="Clear search history"
                hitSlop={8}
                style={({ pressed }) => (pressed ? { opacity: 0.7 } : null)}
              >
                <Text variant="caption" tone="accent">Clear</Text>
              </Pressable>
            ) : null}
          </View>
          <View style={styles.chipCloud}>
            {historyItems.map((item, index) => (
              <Chip
                key={item.query_norm}
                testID={`discover-history-row-${index}`}
                label={item.query.length > 40 ? `${item.query.slice(0, 40)}…` : item.query}
                onPress={() => onHistoryTap(item)}
              />
            ))}
          </View>
          </>
        )}
      </View>
    );
  }

  // view === 'results'
  const results = searchData?.results ?? [];
  const common: ResultsCommonProps = {
    onResultTap,
    impression,
    onRefresh,
    isRefreshing,
    correctedQuery,
    originalQuery,
    onSearchOriginal,
  };
  return (
    <View testID="discover-results" style={styles.results}>
      <FilterChips active={filter} onSelect={onFilterChange} />
      {filter === 'all' ? (
        <BlendedSection results={results} onSeeAll={onFilterChange} common={common} />
      ) : (
        <FilteredResults kind={filter} results={results} common={common} />
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
  zeroResults: { flex: 1 },
  skeletonRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.md,
    paddingVertical: spacing.md,
    paddingHorizontal: spacing.xs,
  },
  skeletonText: { flex: 1, gap: spacing.sm },
  sectionHeader: { letterSpacing: 1 },
  historyHeader: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    marginBottom: spacing.md,
  },
  chipRow: {
    flexDirection: 'row',
    gap: spacing.sm,
    paddingBottom: spacing.md,
    flexWrap: 'wrap',
  },
  chipCloud: { flexDirection: 'row', flexWrap: 'wrap', gap: spacing.sm },
  center: { flex: 1, alignItems: 'center', justifyContent: 'center', padding: spacing['2xl'] },
  centerSub: { marginTop: spacing.xs, marginBottom: spacing.lg },
  emptyCenter: { flex: 1, alignItems: 'center', justifyContent: 'center', gap: spacing.md },
  emptyText: { textAlign: 'center' },
});

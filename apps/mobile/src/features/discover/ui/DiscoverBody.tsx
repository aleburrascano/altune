import type { ReactElement } from 'react';
import { Pressable, StyleSheet, View } from 'react-native';
import { Search } from 'lucide-react-native';

import { Button, Chip, Skeleton, Text, radius, spacing, useTheme } from '@shared/ui';

import type { AsyncView } from '@shared/lib/async-view';
import { describeError } from '@shared/lib/describeError';
import { AsyncSection } from '@shared/ui/AsyncSection';
import { useAnnounceChange } from '@shared/ui/useAnnounceChange';
import { BlendedSection } from './BlendedSection';
import { FilterChips } from './FilterChips';
import { FilteredResults } from './FilteredResults';
import { IncompleteResultsBanner } from './IncompleteResultsBanner';
import { SectionLabel } from './SectionLabel';
import { pressedStyle } from './pressedStyle';
import { SEARCH_UNAVAILABLE_TITLE, _searchAnnouncement } from '../state';
import type {
  DiscoveryResult,
  ResultSection,
  SearchHistoryItem,
} from '@shared/api-client/discovery';
import type { DiscoverView, SearchCorrection } from '../state';
import type { ResultsFilter } from '../hooks/useResultsFilter';
import type { ImpressionHandlers } from '../hooks/useImpressionLogger';
import type { ResultsCommonProps } from './ResultsList';

const SKELETON_ROWS = [0, 1, 2, 3, 4, 5];

interface SearchData {
  results: DiscoveryResult[];
  query_norm?: string;
  top_result?: DiscoveryResult | undefined;
  sections?: ResultSection[];
}

interface DiscoverBodyProps {
  view: DiscoverView;
  searchData: SearchData | undefined;
  /** The shown response is partial (a provider degraded), so results may be incomplete. */
  resultsIncomplete?: boolean | undefined;
  historyItems: SearchHistoryItem[];
  filter: ResultsFilter;
  onFilterChange: (filter: ResultsFilter) => void;
  onHistoryTap: (item: SearchHistoryItem) => void;
  onResultTap: (result: DiscoveryResult, position: number) => void;
  impression: ImpressionHandlers;
  onRetry: () => void;
  searchError?: unknown;
  onEndReached: () => void;
  isFetchingNextPage: boolean;
  onRefresh: () => void;
  isRefreshing: boolean;
  correction: SearchCorrection | null;
  onSearchOriginal: () => void;
  onClearHistory?: (() => void) | undefined;
  nextPageFailed?: boolean | undefined;
  onRetryNextPage?: (() => void) | undefined;
  clearHistoryFailed?: boolean;
}

// Results-rendering fan-out: DiscoverBody → BlendedSection ("all" filter) | FilteredResults
// (one kind) → ResultsList (shared FlatList) → DiscoverRow rows, plus TopResultCard (blended only).
export function DiscoverBody({
  view,
  searchData,
  resultsIncomplete,
  historyItems,
  filter,
  onFilterChange,
  onHistoryTap,
  onResultTap,
  impression,
  onRetry,
  searchError,
  onEndReached,
  isFetchingNextPage,
  onRefresh,
  isRefreshing,
  correction,
  onSearchOriginal,
  onClearHistory,
  nextPageFailed,
  onRetryNextPage,
  clearHistoryFailed,
}: DiscoverBodyProps): ReactElement {
  const theme = useTheme();

  useAnnounceChange(_searchAnnouncement(view, searchData?.results.length ?? 0, resultsIncomplete));

  if (view === 'unavailable') {
    return (
      <View testID="discover-unavailable" style={styles.center}>
        <Text variant="title">{SEARCH_UNAVAILABLE_TITLE}</Text>
        <Text variant="label" tone="secondary" style={styles.centerSub}>
          Check back in a little while.
        </Text>
      </View>
    );
  }

  const { title, body } = describeError(searchError);
  const results = searchData?.results ?? [];
  const common: ResultsCommonProps = {
    onResultTap,
    impression,
    onRefresh,
    isRefreshing,
    onEndReached,
    isFetchingNextPage,
    nextPageFailed,
    onRetryNextPage,
    correction,
    onSearchOriginal,
  };

  const section: AsyncView =
    view === 'loading'
      ? 'loading'
      : view === 'full-error'
        ? 'error'
        : view === 'empty-no-query'
          ? 'empty'
          : 'ready';

  return (
    <AsyncSection
      view={section}
      skeleton={() => (
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
      )}
      error={() => (
        <View testID="discover-full-error" style={styles.center}>
          <Text variant="title">{title}</Text>
          <Text variant="label" tone="secondary" style={styles.centerSub}>
            {body}
          </Text>
          <Button testID="discover-retry" label="Retry" onPress={onRetry} />
        </View>
      )}
      empty={() => (
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
                <SectionLabel>RECENT SEARCHES</SectionLabel>
                {onClearHistory != null ? (
                  <Pressable
                    onPress={onClearHistory}
                    accessibilityRole="button"
                    accessibilityLabel="Clear search history"
                    hitSlop={8}
                    style={({ pressed }) => pressedStyle(pressed)}
                  >
                    <Text variant="caption" tone="accent">
                      Clear
                    </Text>
                  </Pressable>
                ) : null}
              </View>
              {clearHistoryFailed === true ? (
                <Text testID="discover-clear-history-error" variant="caption" tone="secondary">
                  Couldn't clear history. Try again.
                </Text>
              ) : null}
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
      )}
    >
      {view === 'zero-results' ? (
        <View testID="discover-zero-results" style={styles.zeroResults}>
          <FilterChips active={filter} onSelect={onFilterChange} />
          <IncompleteResultsBanner visible={resultsIncomplete} />
          <View style={styles.center}>
            <Text variant="title">No matches</Text>
            <Text variant="label" tone="secondary" style={styles.centerSub}>
              Check spelling or try fewer words.
            </Text>
          </View>
        </View>
      ) : (
        <View testID="discover-results" style={styles.results}>
          <FilterChips active={filter} onSelect={onFilterChange} />
          <IncompleteResultsBanner visible={resultsIncomplete} />
          {filter === 'all' ? (
            <BlendedSection
              sections={searchData?.sections ?? []}
              topResult={searchData?.top_result}
              onSeeAll={onFilterChange}
              common={common}
            />
          ) : (
            <FilteredResults kind={filter} results={results} common={common} />
          )}
        </View>
      )}
    </AsyncSection>
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
  historyHeader: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    marginBottom: spacing.md,
  },
  chipCloud: { flexDirection: 'row', flexWrap: 'wrap', gap: spacing.sm },
  center: { flex: 1, alignItems: 'center', justifyContent: 'center', padding: spacing['2xl'] },
  centerSub: { marginTop: spacing.xs, marginBottom: spacing.lg },
  emptyCenter: { flex: 1, alignItems: 'center', justifyContent: 'center', gap: spacing.md },
  emptyText: { textAlign: 'center' },
});

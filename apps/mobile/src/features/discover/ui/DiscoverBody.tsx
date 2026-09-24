import type { ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';

import { spacing } from '@shared/ui';
import { AsyncSection, type AsyncSectionProps } from '@shared/ui/AsyncSection';
import { useAnnounceChange } from '@shared/ui/useAnnounceChange';
import { BlendedSection } from './BlendedSection';
import { DiscoverFullError, DiscoverUnavailable } from './DiscoverFullError';
import { DiscoverSkeleton } from './DiscoverSkeleton';
import { DiscoverZeroResults } from './DiscoverZeroResults';
import { FilterChips } from './FilterChips';
import { FilteredResults } from './FilteredResults';
import { IncompleteResultsBanner } from './IncompleteResultsBanner';
import { RecentSearches } from './RecentSearches';
import { _searchAnnouncement, asyncViewForDiscoverView } from '../state';
import type {
  DiscoveryResult,
  ResultSection,
  SearchHistoryItem,
} from '@shared/api-client/discovery';
import type { DiscoverView, SearchCorrection } from '../state';
import type { ResultsFilter } from '../hooks/useResultsFilter';
import type { ImpressionHandlers } from '../hooks/useImpressionLogger';

type SlotBuilders = Pick<AsyncSectionProps, 'skeleton' | 'error' | 'empty'>;

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
  onClearHistory: () => void;
  nextPageFailed?: boolean | undefined;
  onRetryNextPage?: (() => void) | undefined;
  clearHistoryFailed?: boolean | undefined;
}

function EmptyNoQuery(props: DiscoverBodyProps): ReactElement {
  return (
    <View testID="discover-empty-no-query" style={styles.list}>
      <RecentSearches {...props} />
    </View>
  );
}

function slotsFor(props: DiscoverBodyProps): SlotBuilders {
  return {
    skeleton: () => <DiscoverSkeleton />,
    error: () => <DiscoverFullError error={props.searchError} onRetry={props.onRetry} />,
    empty: () => <EmptyNoQuery {...props} />,
  };
}

function ResultsContent(props: DiscoverBodyProps): ReactElement {
  const { searchData, filter, onFilterChange } = props;
  if (filter !== 'all') {
    return <FilteredResults kind={filter} results={searchData?.results ?? []} common={props} />;
  }
  return (
    <BlendedSection
      sections={searchData?.sections ?? []}
      topResult={searchData?.top_result}
      onSeeAll={onFilterChange}
      common={props}
    />
  );
}

function ResultsBody(props: DiscoverBodyProps): ReactElement {
  return (
    <View testID="discover-results" style={styles.results}>
      <FilterChips active={props.filter} onSelect={props.onFilterChange} />
      <IncompleteResultsBanner visible={props.resultsIncomplete} />
      <ResultsContent {...props} />
    </View>
  );
}

function ReadyBody(props: DiscoverBodyProps): ReactElement {
  if (props.view !== 'zero-results') return <ResultsBody {...props} />;
  return <DiscoverZeroResults {...props} />;
}

export function DiscoverBody(props: DiscoverBodyProps): ReactElement {
  const count = props.searchData?.results.length ?? 0;
  useAnnounceChange(_searchAnnouncement(props.view, count, props.resultsIncomplete));
  if (props.view === 'unavailable') return <DiscoverUnavailable />;
  return (
    <AsyncSection view={asyncViewForDiscoverView(props.view)} {...slotsFor(props)}>
      <ReadyBody {...props} />
    </AsyncSection>
  );
}

const styles = StyleSheet.create({
  list: { flex: 1, paddingTop: spacing.sm },
  results: { flex: 1 },
});

import type { ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';
import { AsyncSection, type AsyncSectionProps } from '@shared/ui/AsyncSection';
import { useAnnounceChange } from '@shared/ui/useAnnounceChange';
import { BlendedSection } from '@features/discover/ui/BlendedSection';
import { DiscoverFullError, DiscoverUnavailable } from '@features/discover/ui/DiscoverFullError';
import { DiscoverSkeleton } from '@features/discover/ui/DiscoverSkeleton';
import { DiscoverZeroResults } from '@features/discover/ui/DiscoverZeroResults';
import { FilterChips } from '@features/discover/ui/FilterChips';
import { FilteredResults } from '@features/discover/ui/FilteredResults';
import { IncompleteResultsBanner } from '@features/discover/ui/IncompleteResultsBanner';
import { RefreshFailedNotice } from '@features/discover/ui/RefreshFailedNotice';
import { RecentSearches } from '@features/discover/ui/RecentSearches';
import { searchAnnouncement, asyncViewForDiscoverView } from '@features/discover/discoverView';
import type {
  DiscoveryResult,
  ResultSection,
  SearchHistoryItem,
} from '@shared/api-client/discovery';
import type { DiscoverView, SearchCorrection } from '@features/discover/discoverView';
import type { ResultsFilter } from '@features/discover/hooks/useResultsFilter';
import type { ImpressionHandlers } from '@features/discover/hooks/useImpressionLogger';

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
  historyFailed?: boolean | undefined;
  onRetryHistory?: (() => void) | undefined;
  refreshFailed?: boolean | undefined;
}

function slotsFor(props: DiscoverBodyProps): SlotBuilders {
  return {
    skeleton: () => <DiscoverSkeleton />,
    error: () => <DiscoverFullError error={props.searchError} onRetry={props.onRetry} />,
    empty: () => <RecentSearches {...props} />,
  };
}

function BlendedResults(props: DiscoverBodyProps): ReactElement {
  return (
    <BlendedSection
      sections={props.searchData?.sections ?? []}
      topResult={props.searchData?.top_result}
      onSeeAll={props.onFilterChange}
      common={props}
    />
  );
}

function ResultsContent(props: DiscoverBodyProps): ReactElement {
  const { searchData, filter } = props;
  if (filter === 'all') return <BlendedResults {...props} />;
  return <FilteredResults kind={filter} results={searchData?.results ?? []} common={props} />;
}

function ResultsBody(props: DiscoverBodyProps): ReactElement {
  return (
    <View testID="discover-results" style={styles.results}>
      <FilterChips active={props.filter} onSelect={props.onFilterChange} />
      <IncompleteResultsBanner visible={props.resultsIncomplete} />
      <RefreshFailedNotice visible={props.refreshFailed} />
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
  useAnnounceChange(searchAnnouncement(props.view, count, props.resultsIncomplete));
  if (props.view === 'unavailable') return <DiscoverUnavailable />;
  return (
    <AsyncSection view={asyncViewForDiscoverView(props.view)} {...slotsFor(props)}>
      <ReadyBody {...props} />
    </AsyncSection>
  );
}

const styles = StyleSheet.create({
  results: { flex: 1 },
});

import type { ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';
import { AsyncSection, type AsyncSectionProps } from '@shared/ui/async/AsyncSection';
import { useAnnounceChange } from '@shared/ui/useAnnounceChange';
import { BlendedSection } from '@features/discovery/ui/BlendedSection';
import {
  DiscoverFullError,
  DiscoverSkeleton,
  DiscoverUnavailable,
  DiscoverZeroResults,
} from '@features/discovery/ui/DiscoverBodyStates';
import { FilterChips } from '@features/discovery/ui/FilterChips';
import { FilteredResults } from '@features/discovery/ui/FilteredResults';
import { UnreleasedAllHits } from '@features/discovery/ui/unreleased/UnreleasedAllHits';
import { UnreleasedSection } from '@features/discovery/ui/unreleased/UnreleasedSection';
import { IncompleteResultsBanner, RefreshFailedNotice } from '@features/discovery/ui/ResultNotices';
import { RecentSearches } from '@features/discovery/ui/RecentSearches';
import {
  searchAnnouncement,
  asyncViewForDiscoverView,
} from '@features/discovery/domain/discoverView';
import type { ResultsCommonProps } from '@features/discovery/ui/ResultsList';
import type { DiscoveryKind, DiscoveryResult, ResultSection } from '@shared/apiClient/discovery';
import type { UnreleasedSearchHit } from '@shared/apiClient/enrichment';
import type {
  HistorySlice,
  PaginationSlice,
  ResultsSlice,
} from '@features/discovery/hooks/useDiscoverLogic';

type SlotBuilders = Pick<AsyncSectionProps, 'skeleton' | 'error' | 'empty'>;

interface SearchData {
  results: DiscoveryResult[];
  query_norm?: string;
  top_result?: DiscoveryResult | undefined;
  sections?: ResultSection[];
}

type ResultsProps = Omit<
  ResultsSlice,
  'searchData' | 'unreleased' | 'resultsIncomplete' | 'searchError' | 'refreshFailed'
> & {
  searchData: SearchData | undefined;
  unreleased?: UnreleasedSearchHit[] | undefined;
  resultsIncomplete?: boolean | undefined;
  searchError?: unknown;
  refreshFailed?: boolean | undefined;
};

type PaginationProps = Pick<PaginationSlice, 'onEndReached' | 'isFetchingNextPage'> &
  Partial<Pick<PaginationSlice, 'nextPageFailed' | 'onRetryNextPage'>>;

type HistoryProps = Pick<HistorySlice, 'items' | 'onTap' | 'onClear'> &
  Partial<Pick<HistorySlice, 'failed' | 'clearFailed' | 'onRetry'>>;

interface DiscoverBodyProps {
  results: ResultsProps;
  pagination: PaginationProps;
  history: HistoryProps;
}

function paginationCommon(pagination: PaginationProps) {
  return {
    onEndReached: pagination.onEndReached,
    isFetchingNextPage: pagination.isFetchingNextPage,
    nextPageFailed: pagination.nextPageFailed,
    onRetryNextPage: pagination.onRetryNextPage,
  };
}

function resultsCommon(results: ResultsProps) {
  return {
    onResultTap: results.onResultTap,
    impression: results.impression,
    onRefresh: results.onRefresh,
    isRefreshing: results.isRefreshing,
    correction: results.correction,
    onSearchOriginal: results.onSearchOriginal,
  };
}

function commonFor({ results, pagination }: DiscoverBodyProps): ResultsCommonProps {
  return { ...resultsCommon(results), ...paginationCommon(pagination) };
}

function slotsFor(props: DiscoverBodyProps): SlotBuilders {
  const { results, history } = props;
  return {
    skeleton: () => <DiscoverSkeleton />,
    error: () => <DiscoverFullError error={results.searchError} onRetry={results.onRetry} />,
    empty: () => (
      <RecentSearches
        historyItems={history.items}
        onHistoryTap={history.onTap}
        onClearHistory={history.onClear}
        clearHistoryFailed={history.clearFailed}
        historyFailed={history.failed}
        onRetryHistory={history.onRetry}
      />
    ),
  };
}

const NO_HITS: UnreleasedSearchHit[] = [];
const NO_RESULTS: DiscoveryResult[] = [];

function hitsOf(props: DiscoverBodyProps): UnreleasedSearchHit[] {
  return props.results.unreleased ?? NO_HITS;
}

function BlendedResults(props: DiscoverBodyProps): ReactElement {
  const { searchData, setFilter } = props.results;
  return (
    <BlendedSection
      footerExtra={<UnreleasedSection hits={hitsOf(props)} />}
      sections={searchData?.sections ?? []}
      topResult={searchData?.top_result}
      onSeeAll={setFilter}
      common={commonFor(props)}
    />
  );
}

type KindResultsProps = DiscoverBodyProps & { kind: DiscoveryKind };

function KindResults(props: KindResultsProps): ReactElement {
  const results = props.results.searchData?.results ?? NO_RESULTS;
  return <FilteredResults kind={props.kind} results={results} common={commonFor(props)} />;
}

function ResultsContent(props: DiscoverBodyProps): ReactElement {
  const { filter } = props.results;
  if (filter === 'all') return <BlendedResults {...props} />;
  if (filter === 'unreleased') return <UnreleasedAllHits hits={hitsOf(props)} />;
  return <KindResults {...props} kind={filter} />;
}

function ResultsBody(props: DiscoverBodyProps): ReactElement {
  const { results } = props;
  return (
    <View testID="discover-results" style={styles.results}>
      <FilterChips
        active={results.filter}
        onSelect={results.setFilter}
        showUnreleased={hitsOf(props).length > 0}
      />
      <IncompleteResultsBanner visible={results.resultsIncomplete} />
      <RefreshFailedNotice visible={results.refreshFailed} />
      <ResultsContent {...props} />
    </View>
  );
}

function ReadyBody(props: DiscoverBodyProps): ReactElement {
  const { results } = props;
  if (results.view !== 'zero-results' || hitsOf(props).length > 0) {
    return <ResultsBody {...props} />;
  }
  return (
    <DiscoverZeroResults
      filter={results.filter}
      onFilterChange={results.setFilter}
      resultsIncomplete={results.resultsIncomplete}
    />
  );
}

export function DiscoverBody(props: DiscoverBodyProps): ReactElement {
  const { view, searchData, resultsIncomplete } = props.results;
  const count = searchData?.results.length ?? 0;
  useAnnounceChange(searchAnnouncement(view, count, resultsIncomplete));
  if (view === 'unavailable') return <DiscoverUnavailable />;
  return (
    <AsyncSection view={asyncViewForDiscoverView(view)} {...slotsFor(props)}>
      <ReadyBody {...props} />
    </AsyncSection>
  );
}

const styles = StyleSheet.create({
  results: { flex: 1 },
});

import { asyncView, type AsyncView } from '@shared/lib/async-view';
import { countLabel } from '@shared/lib/format';

import type { DiscoverySearchResponse } from '@shared/api-client/discovery';

export type DiscoverView =
  'loading' | 'empty-no-query' | 'results' | 'zero-results' | 'full-error' | 'unavailable';

export const SEARCH_UNAVAILABLE_TITLE = 'Search is temporarily unavailable';

export type SearchCorrection = {
  corrected: string;
  original: string;
};

export type DiscoverHookState = {
  query: string;
  isLoading: boolean;
  data: DiscoverySearchResponse | undefined;
  error: Error | null;
  isUnavailable: boolean;
};

const DISCOVER_VIEW_FOR_ASYNC_VIEW: Record<AsyncView, DiscoverView> = {
  loading: 'loading',
  error: 'full-error',
  empty: 'zero-results',
  ready: 'results',
};

function asyncViewForState(state: DiscoverHookState): AsyncView {
  return asyncView({
    isLoading: state.isLoading && state.data === undefined,
    isError: state.error != null && state.data === undefined,
    isEmpty: state.data !== undefined && state.data.results.length === 0,
  });
}

export function viewForState(state: DiscoverHookState): DiscoverView {
  if (state.isUnavailable) return 'unavailable';
  if (!state.query.trim()) return 'empty-no-query';
  return DISCOVER_VIEW_FOR_ASYNC_VIEW[asyncViewForState(state)];
}

const ASYNC_VIEW_FOR_DISCOVER_VIEW: Record<DiscoverView, AsyncView> = {
  loading: 'loading',
  'full-error': 'error',
  'empty-no-query': 'empty',
  results: 'ready',
  'zero-results': 'ready',
  unavailable: 'ready',
};

export function asyncViewForDiscoverView(view: DiscoverView): AsyncView {
  return ASYNC_VIEW_FOR_DISCOVER_VIEW[view];
}

export function resultsIncompleteForState(state: DiscoverHookState): boolean {
  const view = viewForState(state);
  return (view === 'results' || view === 'zero-results') && state.data?.partial === true;
}

export function correctionForResponse(
  data: DiscoverySearchResponse | undefined,
): SearchCorrection | null {
  const corrected = data?.corrected_query;
  const original = data?.original_query;
  if (!corrected || !original) return null;
  return { corrected, original };
}

function resultsAnnouncement(view: DiscoverView, resultCount: number, suffix: string): string {
  if (view === 'zero-results') return `No matches${suffix}`;
  if (view === 'results') return `${resultCount} ${countLabel(resultCount, 'result')}${suffix}`;
  return '';
}

const INCOMPLETE_SUFFIX = '. Some results may be missing';

export function searchAnnouncement(
  view: DiscoverView,
  resultCount: number,
  resultsIncomplete = false,
): string {
  if (view === 'unavailable') return SEARCH_UNAVAILABLE_TITLE;
  if (view === 'full-error') return 'Search failed';
  const suffix = resultsIncomplete ? INCOMPLETE_SUFFIX : '';
  return resultsAnnouncement(view, resultCount, suffix);
}

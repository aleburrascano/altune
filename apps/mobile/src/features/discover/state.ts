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

export function _viewForState(state: DiscoverHookState): DiscoverView {
  if (state.isUnavailable) {
    return 'unavailable';
  }
  if (!state.query.trim()) {
    return 'empty-no-query';
  }
  const view = asyncView({
    isLoading: state.isLoading && state.data === undefined,
    isError: state.error != null && state.data === undefined,
    isEmpty: state.data !== undefined && state.data.results.length === 0,
  });
  switch (view) {
    case 'loading':
      return 'loading';
    case 'error':
      return 'full-error';
    case 'empty':
      return 'zero-results';
    case 'ready':
      return 'results';
  }
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

export function _resultsIncompleteForState(state: DiscoverHookState): boolean {
  const view = _viewForState(state);
  return (view === 'results' || view === 'zero-results') && state.data?.partial === true;
}

export function _correctionForResponse(
  data: DiscoverySearchResponse | undefined,
): SearchCorrection | null {
  const corrected = data?.corrected_query;
  const original = data?.original_query;
  if (!corrected || !original) return null;
  return { corrected, original };
}

export function _searchAnnouncement(
  view: DiscoverView,
  resultCount: number,
  resultsIncomplete = false,
): string {
  const suffix = resultsIncomplete ? '. Some results may be missing' : '';
  if (view === 'unavailable') return SEARCH_UNAVAILABLE_TITLE;
  if (view === 'zero-results') return `No matches${suffix}`;
  if (view === 'full-error') return 'Search failed';
  if (view === 'results') {
    return `${resultCount} ${countLabel(resultCount, 'result')}${suffix}`;
  }
  return '';
}

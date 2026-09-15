import { asyncView } from '@shared/lib/async-view';

import type { DiscoverySearchResponse } from '@shared/api-client/discovery';

export type DiscoverView = 'loading' | 'empty-no-query' | 'results' | 'zero-results' | 'full-error';

export type DiscoverHookState = {
  query: string;
  isLoading: boolean;
  data: DiscoverySearchResponse | undefined;
  error: Error | null;
};

export function _viewForState(state: DiscoverHookState): DiscoverView {
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

// True when the backend flagged the shown response as `partial` (a provider timed
// out, errored, or was rate limited), so a degraded search can be told apart from
// a healthy one. Only a view that renders the response counts as shown.
export function _resultsIncompleteForState(state: DiscoverHookState): boolean {
  const view = _viewForState(state);
  return (view === 'results' || view === 'zero-results') && state.data?.partial === true;
}

import { asyncView } from '@shared/lib/async-view';

import type {
  DiscoveryKind,
  DiscoveryResult,
  DiscoverySearchResponse,
} from '@shared/api-client/discovery';

export type ResultsFilter = 'all' | DiscoveryKind;

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

const KIND_LABELS: Record<DiscoveryKind, readonly [string, string]> = {
  artist: ['Artist', 'Artists'],
  album: ['Album', 'Albums'],
  track: ['Song', 'Songs'],
};

export function kindLabel(kind: DiscoveryKind, opts?: { plural?: boolean }): string {
  return KIND_LABELS[kind][opts?.plural ? 1 : 0];
}

export function resultKey(result: DiscoveryResult, index: number): string {
  const source = result.sources[0];
  return `${result.kind}-${source?.provider ?? 'x'}-${source?.external_id || `${result.title}-${index}`}`;
}

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

export const SECTION_CAP = 10;

export type GroupedResults = {
  albums: DiscoveryResult[];
  tracks: DiscoveryResult[];
  artists: DiscoveryResult[];
};

export function _groupByKind(results: DiscoveryResult[]): GroupedResults {
  const albums: DiscoveryResult[] = [];
  const tracks: DiscoveryResult[] = [];
  const artists: DiscoveryResult[] = [];
  for (const result of results) {
    if (result.kind === 'album') {
      albums.push(result);
    } else if (result.kind === 'track') {
      tracks.push(result);
    } else {
      artists.push(result);
    }
  }
  return { albums, tracks, artists };
}

export function _topResult(results: DiscoveryResult[]): DiscoveryResult | null {
  return results[0] ?? null;
}

export type SectionKey = 'album' | 'track' | 'artist';

const _sectionKeyOf = (kind: DiscoveryResult['kind']): SectionKey => kind;

export function _sectionOrder(results: DiscoveryResult[]): SectionKey[] {
  const order: SectionKey[] = [];
  const seen = new Set<SectionKey>();
  for (const result of results) {
    const key = _sectionKeyOf(result.kind);
    if (!seen.has(key)) {
      seen.add(key);
      order.push(key);
    }
  }
  return order;
}

export function _cap<T>(items: T[], cap: number = SECTION_CAP): T[] {
  return items.slice(0, cap);
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

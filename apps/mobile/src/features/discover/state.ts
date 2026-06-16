/**
 * Pure state-machine for the discover feature.
 *
 * Slice 44 of discover-music-v1. Mirrors the library feature's
 * _viewForState pattern: pure module (no RN imports) so jest unit tests
 * can import without RN transform overhead.
 */

import type { DiscoveryResult, DiscoverySearchResponse } from '../../shared/api-client/discovery';

export type DiscoverView =
  | 'loading'
  | 'empty-no-query'
  | 'results'
  | 'zero-results'
  | 'full-error';

export type DiscoverHookState = {
  query: string;
  isLoading: boolean;
  data: DiscoverySearchResponse | undefined;
  error: Error | null;
};

/**
 * Derive which view to render. Order of precedence:
 *   empty-no-query (q is blank, no fetch yet)
 *   > loading (q present, fetch in flight, no data yet)
 *   > full-error (q present, error and no fallback data)
 *   > zero-results (q present, data with empty results array)
 *   > results (q present, data with at least one result)
 */
export function _viewForState(state: DiscoverHookState): DiscoverView {
  if (!state.query.trim()) {
    return 'empty-no-query';
  }
  if (state.isLoading && state.data === undefined) {
    return 'loading';
  }
  if (state.error && state.data === undefined) {
    return 'full-error';
  }
  if (state.data !== undefined && state.data.results.length === 0) {
    return 'zero-results';
  }
  return 'results';
}

/** Max items shown per kind-section in the blended "All" view. */
export const SECTION_CAP = 10;

export type GroupedResults = {
  albums: DiscoveryResult[];
  tracks: DiscoveryResult[];
  artists: DiscoveryResult[];
};

/**
 * Partition results by kind, preserving the backend's ranking order within
 * each kind.
 */
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

/** The single highest-ranked entry (backend ranks results[0] first), or null. */
export function _topResult(results: DiscoveryResult[]): DiscoveryResult | null {
  return results[0] ?? null;
}

export type SectionKey = 'album' | 'track' | 'artist';

const _sectionKeyOf = (kind: DiscoveryResult['kind']): SectionKey => kind;

/**
 * Order the blended-view sections by which kind best matches the query: the
 * section whose strongest member appears earliest in the globally-ranked
 * results[] comes first. So a track query shows Songs first, an artist query
 * Artists first. Kinds with no results are omitted.
 */
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

/** First `cap` items — used to cap each section in the blended view. */
export function _cap<T>(items: T[], cap: number = SECTION_CAP): T[] {
  return items.slice(0, cap);
}

/**
 * Pure state-machine for the discover feature.
 *
 * Slice 44 of discover-music-v1. Mirrors the library feature's
 * _viewForState pattern: pure module (no RN imports) so jest unit tests
 * can import without RN transform overhead.
 */

import { asyncView } from '@shared/lib/async-view';

import type { DiscoveryKind, DiscoveryResult, DiscoverySearchResponse } from '@shared/api-client/discovery';

/** The active results filter: the blended "All" view or a single kind. */
export type ResultsFilter = 'all' | DiscoveryKind;

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
 * Derive which view to render. `empty-no-query` is a discover-specific pre-state
 * (no fetch yet); past that, the shared async-view spine drives the loading >
 * error > empty > ready precedence, gated on "no data yet" so a refetch over
 * existing results doesn't flash the spinner. The verdict maps onto discover's
 * vocabulary (ready → results, empty → zero-results, error → full-error).
 */
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

const KIND_LABELS: Record<DiscoveryKind, readonly [string, string]> = {
  artist: ['Artist', 'Artists'],
  album: ['Album', 'Albums'],
  track: ['Song', 'Songs'],
};

/**
 * Display copy for a result kind — the one definition all four surfaces
 * (row, top-result card, filter chips, filtered-empty copy) render from.
 * The UI noun for `track` is "Song" per the discover-music-v2 chips; code
 * identifiers keep the domain noun Track (ubiquitous language).
 */
export function kindLabel(kind: DiscoveryKind, opts?: { plural?: boolean }): string {
  return KIND_LABELS[kind][opts?.plural ? 1 : 0];
}

/**
 * Stable list key for a rendered result row. Provider identity first; falls
 * back to title+index so two same-title results without sources can't collide.
 * One definition — the two lists' hand-rolled keys had already drifted apart.
 */
export function resultKey(result: DiscoveryResult, index: number): string {
  const source = result.sources[0];
  return `${result.kind}-${source?.provider ?? 'x'}-${source?.external_id || `${result.title}-${index}`}`;
}

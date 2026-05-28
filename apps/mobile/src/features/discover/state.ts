/**
 * Pure state-machine for the discover feature.
 *
 * Slice 44 of discover-music-v1. Mirrors the library feature's
 * _viewForState pattern: pure module (no RN imports) so jest unit tests
 * can import without RN transform overhead.
 */

import type { DiscoverySearchResponse } from '../../shared/api-client/discovery';

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

/** True iff any provider on the response is non-OK — gates the partial banner. */
export function _shouldShowPartialBanner(
  data: DiscoverySearchResponse | undefined,
): boolean {
  if (data === undefined) {
    return false;
  }
  return data.partial || data.providers.some((p) => p.status !== 'ok');
}

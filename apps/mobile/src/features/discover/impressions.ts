/**
 * impressions — pure helper for the results_shown impression event.
 *
 * Maps the rendered result slate to the ordered impression rows the backend
 * needs for any show-conditioned metric (CTR@position, position-bias
 * correction). Kept pure (matches the state.ts pattern) so it is unit-testable
 * without rendering a FlatList. The actual visibility gate + emission lives in
 * useImpressionLogger.
 */

import type { DiscoveryResult } from '@shared/api-client/discovery';

export type ImpressionRow = {
  result_signature: string;
  position: number;
  provider: string | null;
  confidence: string;
};

export function buildImpressionRows(
  results: readonly DiscoveryResult[],
): ImpressionRow[] {
  return results.map((r, position) => ({
    result_signature: r.result_signature ?? '',
    position,
    provider: r.sources[0]?.provider ?? null,
    confidence: r.confidence,
  }));
}

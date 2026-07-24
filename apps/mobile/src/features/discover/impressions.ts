import type { DiscoveryResult } from '@shared/api-client/discovery';

export type ImpressionRow = {
  result_signature: string;
  position: number;
  provider: string | null;
  confidence: string;
};

export function buildImpressionRows(results: readonly DiscoveryResult[]): ImpressionRow[] {
  return results.map((r, position) => ({
    result_signature: r.result_signature ?? '',
    position,
    provider: r.sources[0]?.provider ?? null,
    confidence: r.confidence,
  }));
}

import type { DiscoveryResult } from '@shared/api-client/discovery';

export function resultKey(result: DiscoveryResult, index: number): string {
  const source = result.sources[0];
  return `${result.kind}-${source?.provider ?? 'x'}-${source?.external_id || `${result.title}-${index}`}`;
}

import type { DiscoverySource } from '../api-client/discovery';

export function pickSource(
  sources: DiscoverySource[],
  provider: string,
): DiscoverySource | null {
  return sources.find((s) => s.provider === provider) ?? null;
}

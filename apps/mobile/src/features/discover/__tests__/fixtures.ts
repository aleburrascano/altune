import type { DiscoveryResult } from '@shared/api-client/discovery';

// The one complete DiscoveryResult fixture for discover specs. Add new required
// fields here so every spec picks them up; tests vary fields via overrides.
export function resultFixture(overrides: Partial<DiscoveryResult> = {}): DiscoveryResult {
  return {
    kind: 'track',
    title: 'The Title',
    subtitle: null,
    image_url: null,
    confidence: 'high',
    result_signature: 'sig',
    sources: [{ provider: 'spotify', external_id: 'ext-1', url: 'https://x' }],
    extras: {},
    ...overrides,
  };
}

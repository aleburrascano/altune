import { buildImpressionRows } from '../impressions';

import type { DiscoveryResult } from '@shared/api-client/discovery';

function resultFixture(overrides: Partial<DiscoveryResult> = {}): DiscoveryResult {
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

describe('buildImpressionRows projects results into impression rows', () => {
  it('returns an empty array for an empty result set', () => {
    expect(buildImpressionRows([])).toEqual([]);
  });

  it('assigns position from the global array index, not a section-local one', () => {
    const rows = buildImpressionRows([
      resultFixture({ result_signature: 'a' }),
      resultFixture({ result_signature: 'b' }),
      resultFixture({ result_signature: 'c' }),
    ]);

    expect(rows.map((r) => r.position)).toEqual([0, 1, 2]);
  });

  it('echoes the wire result_signature when present', () => {
    const rows = buildImpressionRows([resultFixture({ result_signature: 'wire-sig' })]);

    expect(rows[0]).toEqual({
      result_signature: 'wire-sig',
      position: 0,
      provider: 'spotify',
      confidence: 'high',
    });
  });

  it('substitutes an empty signature when the result carries none', () => {
    const rows = buildImpressionRows([resultFixture({ result_signature: undefined })]);

    expect(rows[0]?.result_signature).toBe('');
  });

  it('takes the provider from the first source', () => {
    const rows = buildImpressionRows([
      resultFixture({ sources: [{ provider: 'tidal', external_id: 'e', url: 'u' }] }),
    ]);

    expect(rows[0]?.provider).toBe('tidal');
  });

  it('reports a null provider when the result has no source', () => {
    const rows = buildImpressionRows([resultFixture({ sources: [] })]);

    expect(rows[0]?.provider).toBeNull();
  });

  it('carries the confidence through unchanged', () => {
    const rows = buildImpressionRows([resultFixture({ confidence: 'low' })]);

    expect(rows[0]?.confidence).toBe('low');
  });
});

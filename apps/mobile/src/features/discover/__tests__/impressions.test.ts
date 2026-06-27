import { buildImpressionRows } from '../impressions';
import type { DiscoveryResult } from '../../../shared/api-client/discovery';

function _result(over: Partial<DiscoveryResult> = {}): DiscoveryResult {
  return {
    kind: 'track',
    title: 'Midnight City',
    subtitle: 'M83',
    image_url: null,
    confidence: 'high',
    result_signature: 'track|midnight city|m83',
    sources: [{ provider: 'deezer', external_id: '1', url: 'https://x' }],
    extras: {},
    ...over,
  };
}

describe('buildImpressionRows', () => {
  it('maps each result to an ordered impression row', () => {
    const rows = buildImpressionRows([
      _result({ result_signature: 'a' }),
      _result({ result_signature: 'b', confidence: 'low' }),
    ]);

    expect(rows).toEqual([
      { result_signature: 'a', position: 0, provider: 'deezer', confidence: 'high' },
      { result_signature: 'b', position: 1, provider: 'deezer', confidence: 'low' },
    ]);
  });

  it('tolerates a missing signature and missing source', () => {
    const rows = buildImpressionRows([
      _result({ result_signature: undefined, sources: [] }),
    ]);

    expect(rows[0]).toEqual({
      result_signature: '',
      position: 0,
      provider: null,
      confidence: 'high',
    });
  });

  it('returns an empty slate for no results', () => {
    expect(buildImpressionRows([])).toEqual([]);
  });
});

import { clearDetailHandoff, getDetailHandoff } from '@shared/lib/detail-handoff';

import { stashHandoffForDetail } from '../tap';
import type { DiscoveryResult } from '../../../shared/api-client/discovery';

function _result(): DiscoveryResult {
  return {
    kind: 'track',
    title: 'Midnight City',
    subtitle: 'M83',
    image_url: null,
    confidence: 'high',
    sources: [],
    extras: {},
  };
}

afterEach(() => {
  clearDetailHandoff();
});

describe('stashHandoffForDetail', () => {
  it('stashes the tapped result and returns the /detail route', () => {
    const result = _result();
    const route = stashHandoffForDetail(result);
    expect(route).toBe('/discover/detail');
    expect(getDetailHandoff()).toBe(result);
  });
});

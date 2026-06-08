/**
 * stashHandoffForDetail — tapping a result stashes it for the detail screen
 * and yields the /detail route (view-result-detail slice 12).
 *
 * Click recording stays a separate fire-and-forget concern in the screen; this
 * helper only owns the handoff + route, so it is testable without rendering.
 */

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

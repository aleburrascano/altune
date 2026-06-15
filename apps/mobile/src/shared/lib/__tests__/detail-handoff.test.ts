import type { DiscoveryResult } from '../../api-client/discovery';

import { clearDetailHandoff, getDetailHandoff, setDetailHandoff } from '../detail-handoff';

const _RESULT: DiscoveryResult = {
  kind: 'track',
  title: 'Test Track',
  subtitle: 'Test Artist',
  image_url: null,
  confidence: 'high',
  sources: [],
  extras: {},
};

describe('detail-handoff', () => {
  afterEach(() => {
    clearDetailHandoff();
  });

  it('returns null when nothing has been set', () => {
    expect(getDetailHandoff()).toBeNull();
  });

  it('roundtrips a set/get', () => {
    setDetailHandoff(_RESULT);
    expect(getDetailHandoff()).toBe(_RESULT);
  });

  it('clears the handoff', () => {
    setDetailHandoff(_RESULT);
    clearDetailHandoff();
    expect(getDetailHandoff()).toBeNull();
  });

  it('overwrites on subsequent set', () => {
    setDetailHandoff(_RESULT);
    const second: DiscoveryResult = { ..._RESULT, title: 'Other' };
    setDetailHandoff(second);
    expect(getDetailHandoff()!.title).toBe('Other');
  });
});

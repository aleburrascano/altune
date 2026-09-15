import { clearDetailHandoffs, detailHref, readDetailHandoff } from '../detail-handoff';

import type { DiscoveryResult } from '@shared/api-client/discovery';

function makeResult(overrides: Partial<DiscoveryResult> = {}): DiscoveryResult {
  return {
    kind: 'track',
    title: 'Track A',
    subtitle: 'Artist A',
    image_url: null,
    confidence: 'high',
    sources: [],
    extras: {},
    ...overrides,
  };
}

beforeEach(() => {
  clearDetailHandoffs();
});

describe('detail-handoff — href carries a keyed handoff to the detail route', () => {
  it('targets the given pathname and names the handoff in a route param', () => {
    const href = detailHref('/library/detail', makeResult());
    expect(href.pathname).toBe('/library/detail');
    expect(typeof href.params.handoff).toBe('string');
  });

  it('reads back the exact result and search id that were handed off', () => {
    const result = makeResult();
    const { params } = detailHref('/discover/detail', result, 'search-1');
    const handoff = readDetailHandoff(params.handoff);
    expect(handoff?.result).toBe(result);
    expect(handoff?.searchId).toBe('search-1');
  });

  it('stores a null search id when none is supplied', () => {
    const { params } = detailHref('/discover/detail', makeResult());
    expect(readDetailHandoff(params.handoff)?.searchId).toBeNull();
  });

  it('is replay-safe: reading the same id twice yields the same handoff', () => {
    const { params } = detailHref('/discover/detail', makeResult());
    expect(readDetailHandoff(params.handoff)).toBe(readDetailHandoff(params.handoff));
  });

  it.each([
    ['a missing param', undefined],
    ['an unknown id', 'never-minted'],
    ['an array param', ['a', 'b']],
  ] as const)('resolves %s to null so the screen redirects', (_label, id) => {
    expect(readDetailHandoff(id as string | string[] | undefined)).toBeNull();
  });
});

describe('detail-handoff — rapid and re-entrant navigation', () => {
  it('two back-to-back handoffs each resolve to their own result, not the last write', () => {
    const resultA = makeResult({ title: 'Track A' });
    const resultB = makeResult({ title: 'Track B' });

    const hrefA = detailHref('/discover/detail', resultA, 'search-a');
    const hrefB = detailHref('/discover/detail', resultB, 'search-b');

    expect(hrefA.params.handoff).not.toBe(hrefB.params.handoff);
    expect(readDetailHandoff(hrefA.params.handoff)).toEqual({
      result: resultA,
      searchId: 'search-a',
    });
    expect(readDetailHandoff(hrefB.params.handoff)).toEqual({
      result: resultB,
      searchId: 'search-b',
    });
  });

  it('a lateral handoff without a search id does not strip the search id of the screen below', () => {
    const below = detailHref('/discover/detail', makeResult(), 'search-a');
    detailHref('/discover/detail', makeResult({ title: 'Lateral' }));
    expect(readDetailHandoff(below.params.handoff)?.searchId).toBe('search-a');
  });

  it('never reuses an id after the registry is cleared', () => {
    const before = detailHref('/discover/detail', makeResult({ title: 'Old' }));
    clearDetailHandoffs();
    const after = detailHref('/discover/detail', makeResult({ title: 'New' }));
    expect(after.params.handoff).not.toBe(before.params.handoff);
    expect(readDetailHandoff(before.params.handoff)).toBeNull();
  });

  it('bounds memory by evicting only the oldest handoffs', () => {
    const first = detailHref('/discover/detail', makeResult({ title: 'first' }));
    let last = first;
    for (let i = 0; i < 100; i += 1) {
      last = detailHref('/discover/detail', makeResult({ title: `t${i}` }));
    }
    expect(readDetailHandoff(first.params.handoff)).toBeNull();
    expect(readDetailHandoff(last.params.handoff)?.result.title).toBe('t99');
  });
});

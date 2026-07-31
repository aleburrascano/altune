import {
  clearDetailHandoff,
  getDetailHandoff,
  getDetailHandoffSearchId,
  setDetailHandoff,
} from '../detail-handoff';

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
  clearDetailHandoff();
});

describe('detail-handoff — module is importable and exercisable with no mocks', () => {
  it('reads and writes without any native or React dependency', () => {
    expect(getDetailHandoff()).toBeNull();
    setDetailHandoff(makeResult());
    expect(getDetailHandoff()).not.toBeNull();
  });
});

describe('detail-handoff — full state x operation matrix', () => {
  it('getDetailHandoff before any set returns null', () => {
    expect(getDetailHandoff()).toBeNull();
  });

  it('getDetailHandoffSearchId before any set returns null', () => {
    expect(getDetailHandoffSearchId()).toBeNull();
  });

  it('clearDetailHandoff on already-empty state stays null for both reads', () => {
    clearDetailHandoff();
    expect(getDetailHandoff()).toBeNull();
    expect(getDetailHandoffSearchId()).toBeNull();
  });

  it('setDetailHandoff with a searchId moves to set-with-searchId: both readers see the write', () => {
    const result = makeResult();
    setDetailHandoff(result, 'search-1');
    expect(getDetailHandoff()).toEqual(result);
    expect(getDetailHandoffSearchId()).toBe('search-1');
  });

  it('setDetailHandoff without a searchId moves to set-without-searchId: result set, searchId null', () => {
    const result = makeResult();
    setDetailHandoff(result);
    expect(getDetailHandoff()).toEqual(result);
    expect(getDetailHandoffSearchId()).toBeNull();
  });

  it('set-with-searchId followed by set-without-searchId resets the searchId to null, not the previous id', () => {
    setDetailHandoff(makeResult({ title: 'Track A' }), 'search-1');
    expect(getDetailHandoffSearchId()).toBe('search-1');

    setDetailHandoff(makeResult({ title: 'Track B' }));
    expect(getDetailHandoff()).toEqual(makeResult({ title: 'Track B' }));
    expect(getDetailHandoffSearchId()).toBeNull();
  });

  it('set-without-searchId followed by set-with-searchId attaches the new id', () => {
    setDetailHandoff(makeResult({ title: 'Track A' }));
    expect(getDetailHandoffSearchId()).toBeNull();

    setDetailHandoff(makeResult({ title: 'Track B' }), 'search-2');
    expect(getDetailHandoff()).toEqual(makeResult({ title: 'Track B' }));
    expect(getDetailHandoffSearchId()).toBe('search-2');
  });

  it('clearDetailHandoff from set-with-searchId returns to never-set for both readers', () => {
    setDetailHandoff(makeResult(), 'search-1');
    clearDetailHandoff();
    expect(getDetailHandoff()).toBeNull();
    expect(getDetailHandoffSearchId()).toBeNull();
  });

  it('clearDetailHandoff from set-without-searchId returns to never-set for both readers', () => {
    setDetailHandoff(makeResult());
    clearDetailHandoff();
    expect(getDetailHandoff()).toBeNull();
    expect(getDetailHandoffSearchId()).toBeNull();
  });
});

describe('detail-handoff — idempotence / replay', () => {
  it('setDetailHandoff applied twice with the same argument is equivalent to applying it once', () => {
    const result = makeResult();
    setDetailHandoff(result, 'search-1');
    setDetailHandoff(result, 'search-1');
    expect(getDetailHandoff()).toEqual(result);
    expect(getDetailHandoffSearchId()).toBe('search-1');
  });

  it('clearDetailHandoff applied twice is equivalent to applying it once', () => {
    setDetailHandoff(makeResult(), 'search-1');
    clearDetailHandoff();
    clearDetailHandoff();
    expect(getDetailHandoff()).toBeNull();
    expect(getDetailHandoffSearchId()).toBeNull();
  });

  it('getDetailHandoff called twice with no write between returns the exact same reference', () => {
    const result = makeResult();
    setDetailHandoff(result);
    const first = getDetailHandoff();
    const second = getDetailHandoff();
    expect(first).toBe(second);
  });
});

describe('detail-handoff — concurrency / ordering across discover, library and lateral nav', () => {
  it('double-tap or lateral nav before the previous read: set(A) then set(B) then get yields B, losing A silently', () => {
    const resultA = makeResult({ title: 'Track A' });
    const resultB = makeResult({ title: 'Track B' });
    setDetailHandoff(resultA);
    setDetailHandoff(resultB);
    expect(getDetailHandoff()).toEqual(resultB);
  });

  it('set(A) then clear then get yields null, not stale A', () => {
    setDetailHandoff(makeResult({ title: 'Track A' }));
    clearDetailHandoff();
    expect(getDetailHandoff()).toBeNull();
  });

  it('set(A) then get then set(B) then get yields A then B in order', () => {
    const resultA = makeResult({ title: 'Track A' });
    const resultB = makeResult({ title: 'Track B' });
    setDetailHandoff(resultA);
    expect(getDetailHandoff()).toEqual(resultA);
    setDetailHandoff(resultB);
    expect(getDetailHandoff()).toEqual(resultB);
  });

  it('both readers observe the same write when nothing writes between them, so a save is never attributed to another search', () => {
    const resultA = makeResult({ title: 'Track A' });
    const resultB = makeResult({ title: 'Track B' });

    setDetailHandoff(resultA, 'search-a');
    expect(getDetailHandoff()).toEqual(resultA);
    expect(getDetailHandoffSearchId()).toBe('search-a');

    setDetailHandoff(resultB, 'search-b');
    expect(getDetailHandoff()).toEqual(resultB);
    expect(getDetailHandoffSearchId()).toBe('search-b');
  });
});

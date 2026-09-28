import { renderHook } from '@testing-library/react-native';
import type { ImperativeRouter } from 'expo-router';

import type { DiscoveryResult } from '@shared/api-client/discovery';
import { clearDetailHandoffs, readDetailHandoff } from '@shared/lib/detail-handoff';

import {
  detailRouteFor,
  featuringRouteFor,
  openDetail,
  tabRootFromSegments,
  useOpenDetail,
} from '../navigation';

jest.mock('expo-router', () => ({ useRouter: jest.fn() }));

function track(title: string): DiscoveryResult {
  return {
    kind: 'track',
    title,
    subtitle: 'Artist',
    image_url: null,
    confidence: 'high',
    sources: [],
    extras: {},
  };
}

beforeEach(() => {
  clearDetailHandoffs();
});

describe('tabRootFromSegments', () => {
  it('reads library from the second segment', () => {
    expect(tabRootFromSegments(['(tabs)', 'library', 'detail'])).toBe('library');
  });

  it('falls back to discover for any other second segment', () => {
    expect(tabRootFromSegments(['(tabs)', 'discover'])).toBe('discover');
    expect(tabRootFromSegments(['(tabs)'])).toBe('discover');
  });
});

describe('detailRouteFor', () => {
  it('builds the detail route under the given tab root', () => {
    expect(detailRouteFor('library')).toBe('/library/detail');
    expect(detailRouteFor('discover')).toBe('/discover/detail');
  });
});

describe('featuringRouteFor', () => {
  it('builds the featuring route matching the library detail route', () => {
    expect(featuringRouteFor('/library/detail')).toBe('/library/featuring');
  });

  it('builds the featuring route matching the discover detail route', () => {
    expect(featuringRouteFor('/discover/detail')).toBe('/discover/featuring');
  });
});

describe('openDetail', () => {
  it('pushes the given route carrying the result, resolvable from the handoff', () => {
    const push = jest.fn();
    const router = { push } as unknown as ImperativeRouter;
    const result = track('Midnight City');

    openDetail(router, '/discover/detail', result);

    expect(push).toHaveBeenCalledTimes(1);
    const [href] = push.mock.calls[0]!;
    expect(href.pathname).toBe('/discover/detail');
    expect(readDetailHandoff(href.params.handoff)?.result).toEqual(result);
  });
});

describe('useOpenDetail', () => {
  it('pushes the same href openDetail does', () => {
    const push = jest.fn();
    jest.spyOn(require('expo-router'), 'useRouter').mockReturnValue({ push });
    const result = track('Midnight City');

    const { result: hook } = renderHook(() => useOpenDetail('/library/detail'));
    hook.current(result);

    expect(push).toHaveBeenCalledTimes(1);
    const [href] = push.mock.calls[0]!;
    expect(href.pathname).toBe('/library/detail');
    expect(readDetailHandoff(href.params.handoff)?.result).toEqual(result);
  });
});

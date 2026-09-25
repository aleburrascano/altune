import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook } from '@testing-library/react-native';

import { useLateralNav } from '../hooks/useLateralNav';

const mockResolveEntityQuery = jest.fn();
jest.mock('../resolve-entity-query', () => ({
  resolveEntityQuery: (...args: unknown[]) => mockResolveEntityQuery(...args),
}));

jest.mock('../navigation', () => ({
  tabRootFromSegments: () => 'discover',
  detailRouteFor: () => '/discover/detail',
  openDetail: jest.fn(),
}));

jest.mock('expo-router', () => ({
  useRouter: () => ({ push: jest.fn(), replace: jest.fn() }),
  useSegments: () => ['(tabs)', 'discover'],
}));

describe('logging a failed lateral navigation', () => {
  // These detail hooks each have a silent failure path: the failure reason is
  // discarded with no log, so a real incident can't be told apart from a one-off
  // without a live repro. Each test drives its hook into that failure path and
  // asserts the site now logs enough to diagnose it (status/provider/artist, the
  // save error + track identity, the lateral-nav query/kind + error, the entity a
  // failed discovery search was looking for).

  function createWrapper(queryClient: QueryClient) {
    return function Wrapper({ children }: { children: React.ReactNode }) {
      return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
    };
  }

  function freshClient() {
    return new QueryClient({ defaultOptions: { queries: { retry: false } } });
  }

  let warnSpy: jest.SpyInstance;

  beforeEach(() => {
    jest.clearAllMocks();
    warnSpy = jest.spyOn(console, 'warn').mockImplementation(() => {});
  });

  afterEach(() => {
    warnSpy.mockRestore();
  });

  describe('useLateralNav logs non-"not found" fetch failures', () => {
    it('logs the query/kind and error when the resolve fetch throws', async () => {
      mockResolveEntityQuery.mockReturnValue({
        queryKey: ['resolve-entity', 'artist', 'Boom', 1],
        queryFn: () => Promise.reject(new Error('transport failed')),
      });

      const { result } = renderHook(() => useLateralNav(), {
        wrapper: createWrapper(freshClient()),
      });

      await act(async () => {
        await result.current.navigateTo('Boom', 'artist');
      });

      expect(warnSpy).toHaveBeenCalledWith(
        '[detail] lateral nav fetch failed',
        expect.objectContaining({
          query: 'Boom',
          kind: 'artist',
          error: 'transport failed',
        }),
      );
      // The visible "not found" path stays silent; only real failures log.
      expect(result.current.state).toBe('idle');
    });

    it('does not log the ordinary "not found" result', async () => {
      mockResolveEntityQuery.mockReturnValue({
        queryKey: ['resolve-entity', 'artist', 'Nobody', 1],
        queryFn: () => Promise.resolve([]),
      });

      const { result } = renderHook(() => useLateralNav(), {
        wrapper: createWrapper(freshClient()),
      });

      await act(async () => {
        await result.current.navigateTo('Nobody', 'artist');
      });

      expect(warnSpy).not.toHaveBeenCalled();
      expect(result.current.error).toContain('not found');
    });
  });
});

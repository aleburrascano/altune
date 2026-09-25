import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { renderHook, waitFor } from '@testing-library/react-native';

import { useArtistDiscovery } from '../hooks/useArtistDiscovery';

const mockResolveEntityQuery = jest.fn();
jest.mock('../resolve-entity-query', () => ({
  resolveEntityQuery: (...args: unknown[]) => mockResolveEntityQuery(...args),
}));

describe('logging a failed search', () => {
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

  describe('useArtistDiscovery logs the artist its search step was looking for', () => {
    it('logs the artist name and reason when the search fails', async () => {
      mockResolveEntityQuery.mockReturnValue({
        queryKey: ['resolve-entity', 'artist', 'Boards of Canada', 1],
        queryFn: () => Promise.reject(new Error('search transport failed')),
      });

      const { result } = renderHook(
        () => useArtistDiscovery({ artistName: 'Boards of Canada', enabled: true }),
        { wrapper: createWrapper(freshClient()) },
      );

      await waitFor(() => expect(result.current.isError).toBe(true), { timeout: 5000 });

      expect(warnSpy).toHaveBeenCalledWith(
        '[detail] discovery search failed',
        expect.objectContaining({
          kind: 'artist',
          title: 'Boards of Canada',
          artist: null,
          error: 'search transport failed',
        }),
      );
    });
  });
});

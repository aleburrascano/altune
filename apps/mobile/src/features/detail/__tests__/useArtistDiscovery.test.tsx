import { renderHook, waitFor } from '@testing-library/react-native';

import { useArtistDiscovery } from '@/features/detail/hooks/useArtistDiscovery';
import { createTestQueryClient, createWrapper } from '@/features/detail/__tests__/support/queryHarness';

const mockResolveEntityQuery = jest.fn();
jest.mock('@/features/detail/domain/resolveEntityQuery', () => ({
  resolveEntityQuery: (...args: unknown[]) => mockResolveEntityQuery(...args),
}));

describe('logging a failed search', () => {
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
        { wrapper: createWrapper(createTestQueryClient()) },
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

import { act, renderHook } from '@testing-library/react-native';

import { useLateralNav } from '../hooks/useLateralNav';
import { readDetailHandoff } from '@shared/lib/detail-handoff';
import { createTestQueryClient, createWrapper } from './support/queryHarness';

const mockResolveEntityQuery = jest.fn();
jest.mock('../resolve-entity-query', () => ({
  resolveEntityQuery: (...args: unknown[]) => mockResolveEntityQuery(...args),
}));

const mockPush = jest.fn();
jest.mock('expo-router', () => ({
  useRouter: () => ({ push: mockPush, replace: jest.fn() }),
}));

describe('logging a failed lateral navigation', () => {
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

      const { result } = renderHook(() => useLateralNav('/discover/detail'), {
        wrapper: createWrapper(createTestQueryClient()),
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
      expect(result.current.state).toBe('idle');
    });

    it('does not log the ordinary "not found" result', async () => {
      mockResolveEntityQuery.mockReturnValue({
        queryKey: ['resolve-entity', 'artist', 'Nobody', 1],
        queryFn: () => Promise.resolve([]),
      });

      const { result } = renderHook(() => useLateralNav('/discover/detail'), {
        wrapper: createWrapper(createTestQueryClient()),
      });

      await act(async () => {
        await result.current.navigateTo('Nobody', 'artist');
      });

      expect(warnSpy).not.toHaveBeenCalled();
      expect(result.current.error).toContain('not found');
    });
  });
});

describe('useLateralNav navigateTo on a hit', () => {
  beforeEach(() => {
    jest.clearAllMocks();
  });

  it('calls openDetail with the discover detail route and the first result', async () => {
    const firstResult = { id: 'artist-1' };
    const secondResult = { id: 'artist-2' };
    mockResolveEntityQuery.mockReturnValue({
      queryKey: ['resolve-entity', 'artist', 'Boom', 1],
      queryFn: () => Promise.resolve([firstResult, secondResult]),
    });

    const { result } = renderHook(() => useLateralNav('/discover/detail'), {
      wrapper: createWrapper(createTestQueryClient()),
    });

    await act(async () => {
      await result.current.navigateTo('Boom', 'artist');
    });

    const href = mockPush.mock.calls[0][0];
    expect(href.pathname).toBe('/discover/detail');
    expect(readDetailHandoff(href.params.handoff)?.result).toBe(firstResult);
  });
});

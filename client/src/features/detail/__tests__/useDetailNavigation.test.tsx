import { act, renderHook } from '@testing-library/react-native';

import type { DiscoveryResult } from '@shared/apiClient/discovery';
import { readDetailHandoff } from '@shared/navigation/detailHandoff';

import { useLateralNav } from '@features/detail/hooks/useDetailNavigation';
import type { DetailPort } from '@features/detail/ports/detailPort';
import { createMemoryDetailPort } from '@features/detail/__tests__/memoryDetailPort';
import { createPortWrapper, createTestQueryClient } from '@testing/queryHarness';

const mockPush = jest.fn();

jest.mock('expo-router', () => ({
  useRouter: () => ({ push: mockPush, replace: jest.fn() }),
}));

function artist(title: string): DiscoveryResult {
  return {
    kind: 'artist',
    title,
    subtitle: null,
    image_url: null,
    confidence: 'high',
    sources: [{ provider: 'deezer', external_id: title, url: `https://deezer/${title}` }],
    extras: {},
  };
}

type Search = { q: string; kinds: readonly string[] | undefined; limit: number | undefined };
type Searches = Search[];
type CountingPort = { port: DetailPort; searches: Searches };

function countingPort(port: DetailPort): CountingPort {
  const searches: Searches = [];
  return {
    searches,
    port: {
      ...port,
      searchDiscovery: (params, signal) => {
        searches.push({ q: params.q, kinds: params.kinds, limit: params.limit });
        return port.searchDiscovery(params, signal);
      },
    },
  };
}

function seeded(discovery: DiscoveryResult[]) {
  return countingPort(createMemoryDetailPort({ discovery }));
}

function failing(message: string) {
  return countingPort({
    ...createMemoryDetailPort(),
    searchDiscovery: () => Promise.reject(new Error(message)),
  });
}

function renderLateralNav(port: DetailPort) {
  return renderHook(() => useLateralNav('/discover/detail'), {
    wrapper: createPortWrapper(createTestQueryClient(), { detail: port }),
  });
}

function pushedResults(): (DiscoveryResult | undefined)[] {
  return mockPush.mock.calls.map(([href]) => readDetailHandoff(href.params.handoff)?.result);
}

describe('useLateralNav', () => {
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
        const { result } = renderLateralNav(failing('transport failed').port);

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
        expect(mockPush).not.toHaveBeenCalled();
      });

      it('does not log the ordinary "not found" result', async () => {
        const { result } = renderLateralNav(seeded([]).port);

        await act(async () => {
          await result.current.navigateTo('Nobody', 'artist');
        });

        expect(warnSpy).not.toHaveBeenCalled();
        expect(result.current.error).toBe('Artist not found: "Nobody"');
        expect(mockPush).not.toHaveBeenCalled();
      });
    });
  });

  describe('useLateralNav navigateTo on a hit', () => {
    beforeEach(() => {
      jest.clearAllMocks();
    });

    it('searches for one result of the kind and opens it on the discover detail route', async () => {
      const first = artist('Boom');
      const { port, searches } = seeded([first, artist('Boom Tribute')]);
      const { result } = renderLateralNav(port);

      await act(async () => {
        await result.current.navigateTo('Boom', 'artist');
      });

      expect(searches).toEqual([{ q: 'Boom', kinds: ['artist'], limit: 1 }]);
      expect(mockPush).toHaveBeenCalledTimes(1);
      expect(mockPush.mock.calls[0][0].pathname).toBe('/discover/detail');
      expect(pushedResults()).toEqual([first]);
    });
  });

  describe('useLateralNav after a pick or a failure', () => {
    beforeEach(() => {
      jest.clearAllMocks();
    });

    it('releases the search lock after a pick so a second navigateTo looks up again', async () => {
      const found = artist('Boom');
      const { port, searches } = seeded([found]);
      const { result } = renderLateralNav(port);

      await act(async () => {
        await result.current.navigateTo('Boom', 'artist');
      });
      expect(result.current.state).toBe('idle');

      await act(async () => {
        await result.current.navigateTo('Other', 'artist');
      });
      expect(searches.map((s) => s.q)).toEqual(['Boom', 'Other']);
      expect(pushedResults()).toEqual([found, found]);
    });

    it('shows a user-facing message when the lookup rejects', async () => {
      const warnSpy = jest.spyOn(console, 'warn').mockImplementation(() => {});
      const { result } = renderLateralNav(failing('transport failed').port);

      await act(async () => {
        await result.current.navigateTo('Boom', 'artist');
      });

      expect(result.current.error).toBe("Couldn't search, try again");
      warnSpy.mockRestore();
    });
  });
});

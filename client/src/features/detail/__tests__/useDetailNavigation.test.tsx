import { act, renderHook } from '@testing-library/react-native';
import type { DiscoveryResult } from '@shared/catalog';
import { readDetailHandoff } from '@shared/navigation';

import { useLateralNav, createMemoryDetailPort } from '@features/detail';
import type { DetailPort, DetailWarning, RecordingDetailPort } from '@features/detail';
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
type CountingPort = { port: DetailPort; searches: Searches; warnings: DetailWarning[] };

function countingPort(port: RecordingDetailPort): CountingPort {
  const searches: Searches = [];
  return {
    searches,
    warnings: port.warnings,
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
    beforeEach(() => {
      jest.clearAllMocks();
    });

    describe('useLateralNav logs non-"not found" fetch failures', () => {
      it('logs the query/kind and error when the resolve fetch throws', async () => {
        const { port, warnings } = failing('transport failed');
        const { result } = renderLateralNav(port);

        await act(async () => {
          await result.current.navigateTo('Boom', 'artist');
        });

        expect(warnings).toContainEqual([
          '[detail] lateral nav fetch failed',
          expect.objectContaining({
            query: 'Boom',
            kind: 'artist',
            error: 'transport failed',
          }),
        ]);
        expect(result.current.state).toBe('idle');
        expect(mockPush).not.toHaveBeenCalled();
      });

      it('does not log the ordinary "not found" result', async () => {
        const { port, warnings } = seeded([]);
        const { result } = renderLateralNav(port);

        await act(async () => {
          await result.current.navigateTo('Nobody', 'artist');
        });

        expect(warnings).toEqual([]);
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
      const { result } = renderLateralNav(failing('transport failed').port);

      await act(async () => {
        await result.current.navigateTo('Boom', 'artist');
      });

      expect(result.current.error).toBe("Couldn't search, try again");
    });
  });
});

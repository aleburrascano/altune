import { renderHook, waitFor } from '@testing-library/react-native';

import { useAlbumDiscovery, useArtistDiscovery } from '@/features/detail/hooks/useEntityDiscovery';
import {
  createTestQueryClient,
  createWrapper,
  signInTestUser,
} from '@/features/detail/__tests__/support/queryHarness';

const { __http } = require('../../../../jest/doubles/fetch.js');

describe('useAlbumDiscovery', () => {
  describe('bounding the fetch', () => {
    const ALBUM_TRACKS_PATH = 'GET /v1/discovery/albums/spotify/album-1/tracks';

    const emptyContent = {
      items: [],
      provider: 'spotify',
      status: 'ok',
      latency_ms: 3,
    };

    beforeEach(async () => {
      await signInTestUser();
    });

    describe('useAlbumDiscovery bounds the album track fetch', () => {
      it('caps the discovery-driven track request with an explicit limit', async () => {
        __http.reply('GET /v1/discovery/search', {
          status: 200,
          json: {
            query: 'rumours fleetwood mac',
            query_norm: 'rumours fleetwood mac',
            results: [
              {
                kind: 'album',
                title: 'Rumours',
                subtitle: 'Fleetwood Mac',
                image_url: null,
                confidence: 'high',
                sources: [
                  { provider: 'spotify', external_id: 'album-1', url: 'https://s.example/1' },
                ],
                extras: {},
              },
            ],
            sections: [],
            providers: [],
            partial: false,
            cache: { hit: false, fetched_at: null },
            total: 1,
            offset: 0,
            has_more: false,
          },
        });
        __http.reply(ALBUM_TRACKS_PATH, { status: 200, json: emptyContent });
        const queryClient = createTestQueryClient();

        const { result } = renderHook(
          () =>
            useAlbumDiscovery({ albumTitle: 'Rumours', artist: 'Fleetwood Mac', enabled: true }),
          { wrapper: createWrapper(queryClient) },
        );

        await waitFor(() =>
          expect(__http.last()?.path).toBe('/v1/discovery/albums/spotify/album-1/tracks'),
        );
        await waitFor(() => expect(result.current.isLoading).toBe(false));

        const params = new URLSearchParams(__http.last().query);
        expect(params.get('limit')).toBe('100');
      });
    });
  });

  describe('logging a failed search', () => {
    let warnSpy: jest.SpyInstance;

    beforeEach(async () => {
      jest.clearAllMocks();
      await signInTestUser();
      warnSpy = jest.spyOn(console, 'warn').mockImplementation(() => {});
    });

    afterEach(() => {
      warnSpy.mockRestore();
    });

    describe('useAlbumDiscovery logs the album its search step was looking for', () => {
      it('logs the title, artist and reason when the search step fails', async () => {
        __http.fail('GET /v1/discovery/search');

        const { result } = renderHook(
          () =>
            useAlbumDiscovery({ albumTitle: 'Rumours', artist: 'Fleetwood Mac', enabled: true }),
          { wrapper: createWrapper(createTestQueryClient()) },
        );

        await waitFor(() => expect(result.current.isError).toBe(true), { timeout: 5000 });

        expect(warnSpy).toHaveBeenCalledWith(
          '[detail] discovery search failed',
          expect.objectContaining({
            kind: 'album',
            title: 'Rumours',
            artist: 'Fleetwood Mac',
            error: 'API /v1/discovery/search is unreachable',
          }),
        );
        expect(
          warnSpy.mock.calls.filter(([message]) => message === '[detail] discovery search failed'),
        ).toHaveLength(1);
      });
    });
  });

  describe('useAlbumDiscovery reopened against the 30-minute content cache window', () => {
    const DISCOVERED_TRACKS_PATH = 'GET /v1/discovery/albums/spotify/album-cache/tracks';
    const { act } = require('@testing-library/react-native');

    beforeEach(async () => {
      await signInTestUser();
    });

    it.each([
      ['serves the cached tracklist without a request 1 ms before 30 minutes', 1_799_999, 1],
      ['asks the server again for the tracklist 1 ms after 30 minutes', 1_800_001, 2],
    ])('%s', async (_behaviour, elapsedMs, expectedRequests) => {
      let now = 1_700_000_000_000;
      jest.spyOn(Date, 'now').mockImplementation(() => now);
      __http.reply('GET /v1/discovery/search', {
        status: 200,
        json: {
          query: 'tusk fleetwood mac',
          query_norm: 'tusk fleetwood mac',
          results: [
            {
              kind: 'album',
              title: 'Tusk',
              subtitle: 'Fleetwood Mac',
              image_url: null,
              confidence: 'high',
              sources: [
                { provider: 'spotify', external_id: 'album-cache', url: 'https://s.example/c' },
              ],
              extras: {},
            },
          ],
          sections: [],
          providers: [],
          partial: false,
          cache: { hit: false, fetched_at: null },
          total: 1,
          offset: 0,
          has_more: false,
        },
      });
      __http.reply(DISCOVERED_TRACKS_PATH, {
        status: 200,
        json: { items: [], provider_name: 'spotify', status: 'ok' },
      });
      const wrapper = createWrapper(createTestQueryClient());
      const useHook = () =>
        useAlbumDiscovery({ albumTitle: 'Tusk', artist: 'Fleetwood Mac', enabled: true });

      const first = renderHook(useHook, { wrapper });
      await waitFor(() => expect(__http.countFor(DISCOVERED_TRACKS_PATH)).toBe(1));
      await waitFor(() => expect(first.result.current.isLoading).toBe(false));
      expect(first.result.current.isError).toBe(false);
      first.unmount();
      now += elapsedMs;
      renderHook(useHook, { wrapper });
      await act(async () => {
        await new Promise((resolve) => setImmediate(resolve));
      });

      expect(__http.countFor(DISCOVERED_TRACKS_PATH)).toBe(expectedRequests);
    });
  });
});

describe('useArtistDiscovery', () => {
  describe('logging a failed search', () => {
    let warnSpy: jest.SpyInstance;

    beforeEach(async () => {
      jest.clearAllMocks();
      await signInTestUser();
      warnSpy = jest.spyOn(console, 'warn').mockImplementation(() => {});
    });

    afterEach(() => {
      warnSpy.mockRestore();
    });

    describe('useArtistDiscovery logs the artist its search step was looking for', () => {
      it('logs the artist name and reason when the search fails', async () => {
        __http.fail('GET /v1/discovery/search');

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
            error: 'API /v1/discovery/search is unreachable',
          }),
        );
      });
    });
  });
});

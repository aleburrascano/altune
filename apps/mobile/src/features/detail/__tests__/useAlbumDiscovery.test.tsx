import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { renderHook, waitFor } from '@testing-library/react-native';

import { supabase } from '@shared/auth/supabaseClient';

import { useAlbumDiscovery } from '../hooks/useAlbumDiscovery';

const { __http } = require('../../../../jest/doubles/fetch.js');

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn() } },
}));

const mockResolveEntityQuery = jest.fn();
jest.mock('../resolve-entity-query', () => ({
  resolveEntityQuery: (...args: unknown[]) => mockResolveEntityQuery(...args),
}));

function createWrapper(queryClient: QueryClient) {
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
  };
}

function freshClient() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } });
}

describe('bounding the fetch', () => {
  const ALBUM_TRACKS_PATH = 'GET /v1/discovery/albums/spotify/album-1/tracks';

  const emptyContent = {
    items: [],
    provider: 'spotify',
    status: 'ok',
    latency_ms: 3,
  };

  beforeEach(() => {
    mockResolveEntityQuery.mockImplementation(
      jest.requireActual('../resolve-entity-query').resolveEntityQuery,
    );
  });

  beforeEach(() => {
    (supabase.auth.getSession as jest.Mock).mockReset().mockResolvedValue({
      data: { session: { access_token: 'tok' } },
      error: null,
    });
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
              sources: [{ provider: 'spotify', external_id: 'album-1', url: 'https://s.example/1' }],
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
      const queryClient = freshClient();

      const { result } = renderHook(
        () => useAlbumDiscovery({ albumTitle: 'Rumours', artist: 'Fleetwood Mac', enabled: true }),
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
  // These detail hooks each have a silent failure path: the failure reason is
  // discarded with no log, so a real incident can't be told apart from a one-off
  // without a live repro. Each test drives its hook into that failure path and
  // asserts the site now logs enough to diagnose it (status/provider/artist, the
  // save error + track identity, the lateral-nav query/kind + error, the entity a
  // failed discovery search was looking for).

  let warnSpy: jest.SpyInstance;

  beforeEach(() => {
    jest.clearAllMocks();
    warnSpy = jest.spyOn(console, 'warn').mockImplementation(() => {});
  });

  afterEach(() => {
    warnSpy.mockRestore();
  });

  // Issue #1660: the resolve request's own log strips the query string, so a
  // failed discography search named no entity at all.
  describe('useAlbumDiscovery logs the album its search step was looking for', () => {
    it('logs the title, artist and reason when the search step fails', async () => {
      mockResolveEntityQuery.mockReturnValue({
        queryKey: ['resolve-entity', 'album', 'Rumours Fleetwood Mac', 1],
        queryFn: () => Promise.reject(new Error('search transport failed')),
      });

      const { result } = renderHook(
        () => useAlbumDiscovery({ albumTitle: 'Rumours', artist: 'Fleetwood Mac', enabled: true }),
        { wrapper: createWrapper(freshClient()) },
      );

      await waitFor(() => expect(result.current.isError).toBe(true), { timeout: 5000 });

      expect(warnSpy).toHaveBeenCalledWith(
        '[detail] discovery search failed',
        expect.objectContaining({
          kind: 'album',
          title: 'Rumours',
          artist: 'Fleetwood Mac',
          error: 'search transport failed',
        }),
      );
      // One failure, one line: a re-render must not re-log it.
      expect(warnSpy).toHaveBeenCalledTimes(1);
    });
  });
});

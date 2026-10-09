import { QueryClient } from '@tanstack/react-query';

import { act, renderHook, waitFor } from '@testing-library/react-native';
import { isRetryable } from '@shared/errors';
import { supabase } from '@shared/auth/supabaseClient';
import type { DiscoverySource } from '@shared/api-client/discovery';
import { createMemoryFileStore } from '@shared/files/__tests__/memoryFileStore';
import { applyKillSwitches, setKillSwitchFileStore } from '@shared/killSwitch/killSwitch';

import { useAlbumTracks } from '@/features/detail/hooks/useAlbumTracks';
import { useArtistContent } from '@/features/detail/hooks/useArtistContent';
import { useRelatedTracks } from '@/features/detail/hooks/useRelatedTracks';
import {
  createWrapper,
  mockSupabaseSession,
  createTestQueryClient,
} from '@/features/detail/__tests__/support/queryHarness';
import { useAlbumDiscovery, useArtistDiscovery } from '@/features/detail/hooks/useEntityDiscovery';
import {
  useDeezerEnrichment,
  useEnrichment,
  useLastFmEnrichment,
} from '@/features/detail/hooks/useDetailEnrichments';
import { useLateralNav } from '@/features/detail/hooks/useDetailNavigation';
import { useResolveMissingSources } from '@/features/detail/hooks/useResolveMissingSources';

const { __http } = require('../../../../jest/doubles/fetch.js');

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn() } },
}));

jest.mock('expo-router', () => ({
  useRouter: () => ({ push: jest.fn() }),
  useSegments: () => ['(tabs)', 'discover'],
}));

describe('useContentFetchRetry', () => {
  function appQueryClient() {
    return new QueryClient({
      defaultOptions: {
        queries: { retry: (failureCount, error) => isRetryable(error) && failureCount < 5 },
      },
    });
  }

  const ALBUM_PATH = 'GET /v1/discovery/albums/spotify/album-1/tracks';

  const ARTIST_PATH = 'GET /v1/discovery/artists/spotify/artist-1/content';

  const RELATED_PATH = 'GET /v1/discovery/tracks/soundcloud/sc-1/related';

  const artistSources: DiscoverySource[] = [
    { provider: 'spotify', external_id: 'artist-1' } as DiscoverySource,
  ];

  const trackSources: DiscoverySource[] = [
    { provider: 'soundcloud', external_id: 'sc-1' } as DiscoverySource,
  ];

  const hooks = [
    {
      name: 'useAlbumTracks',
      path: ALBUM_PATH,
      use: () => useAlbumTracks({ provider: 'spotify', externalId: 'album-1' }).isError,
      useFailure: () => useAlbumTracks({ provider: 'spotify', externalId: 'album-1' }).failure,
    },
    {
      name: 'useArtistContent',
      path: ARTIST_PATH,
      use: () =>
        useArtistContent({ sources: artistSources, artistName: 'A' }).tracksFailure !== null,
      useFailure: () => useArtistContent({ sources: artistSources, artistName: 'A' }).tracksFailure,
    },
    {
      name: 'useRelatedTracks',
      path: RELATED_PATH,
      use: () => useRelatedTracks({ sources: trackSources }).isError,
      useFailure: () => useRelatedTracks({ sources: trackSources }).failure,
    },
  ];

  const settledFailures = [
    { status: 503, code: 'discovery.provider_circuit_open' },
    { status: 502, code: 'discovery.provider_error' },
    { status: 504, code: 'discovery.provider_timeout' },
    { status: 503, code: 'discovery.provider_rate_limited' },
  ];

  async function renderWithPastBackoff<T>(use: () => T): Promise<T> {
    const queryClient = appQueryClient();
    const { result, unmount } = renderHook(use, { wrapper: createWrapper(queryClient) });
    await act(async () => {
      await jest.advanceTimersByTimeAsync(60_000);
    });
    const state = result.current;
    unmount();
    queryClient.clear();
    return state;
  }

  let warn: jest.SpyInstance;

  beforeEach(() => {
    jest.useFakeTimers();
    warn = jest.spyOn(console, 'warn').mockImplementation(() => {});
    (supabase.auth.getSession as jest.Mock).mockReset().mockResolvedValue(mockSupabaseSession());
  });

  afterEach(() => {
    jest.useRealTimers();
    warn.mockRestore();
  });

  describe.each(hooks)('$name fails fast on a settled content failure', ({ path, use }) => {
    it.each(settledFailures)('shows the error after one request for $status $code', async (f) => {
      __http.reply(path, { status: f.status, json: { status: 'error', code: f.code } });

      const isError = await renderWithPastBackoff(use);

      expect(isError).toBe(true);
      expect(__http.countFor(path)).toBe(1);
    });

    it('still retries a 5xx without a discovery code', async () => {
      __http.replyOnce(path, { status: 500, json: { code: 'internal' } });
      __http.reply(path, { status: 500, json: { code: 'internal' } });

      await renderWithPastBackoff(use);

      expect(__http.countFor(path)).toBeGreaterThan(1);
    });
  });

  describe.each(hooks)('$name reports why the content fetch failed', ({ path, useFailure }) => {
    it.each(settledFailures)('classifies $status $code as settled', async (f) => {
      __http.reply(path, { status: f.status, json: { status: 'error', code: f.code } });

      expect(await renderWithPastBackoff(useFailure)).toBe('settled');
    });

    it('classifies an unreachable server as transient', async () => {
      __http.fail(path);

      expect(await renderWithPastBackoff(useFailure)).toBe('transient');
    });
  });
});

describe('detail fetch gate', () => {
  const ALBUM_TRACKS_PATH = 'GET /v1/discovery/albums/spotify/album-1/tracks';

  const spotifyAlbum = { provider: 'spotify', external_id: 'album-1', url: 'https://s.example/1' };

  const soundcloudTrack = {
    provider: 'soundcloud',
    external_id: 'track-1',
    url: 'https://s.example/t',
  };

  function switchDetailFetches(enabled: boolean): void {
    act(() => applyKillSwitches({ detail_enrichment_enabled: enabled }));
  }

  async function settle(): Promise<void> {
    await act(async () => {
      await Promise.resolve();
    });
  }

  beforeEach(() => {
    (supabase.auth.getSession as jest.Mock).mockReset().mockResolvedValue(mockSupabaseSession());
    setKillSwitchFileStore(createMemoryFileStore());
    jest.spyOn(console, 'warn').mockImplementation(() => undefined);
    __http.replyAll({
      status: 200,
      json: { items: [], results: [], provider: 'spotify', status: 'ok' },
    });
  });

  afterEach(() => {
    setKillSwitchFileStore();
    jest.restoreAllMocks();
  });

  type GatedHookCase = {
    hook: string;
    useGatedHook: () => Record<string, unknown>;
    emptyResult: Record<string, unknown>;
  };

  const gatedHooks: GatedHookCase[] = [
    {
      hook: 'useEnrichment',
      useGatedHook: () =>
        useEnrichment({ kind: 'album', title: 'Rumours', subtitle: 'Fleetwood Mac' }),
      emptyResult: { enrichment: null, isError: false },
    },
    {
      hook: 'useDeezerEnrichment',
      useGatedHook: () =>
        useDeezerEnrichment({ kind: 'album', title: 'Rumours', subtitle: 'Fleetwood Mac' }),
      emptyResult: { enrichment: null, isError: false },
    },
    {
      hook: 'useLastFmEnrichment',
      useGatedHook: () =>
        useLastFmEnrichment({ kind: 'artist', title: 'Fleetwood Mac', subtitle: null }),
      emptyResult: { enrichment: null, isError: false },
    },
    {
      hook: 'useAlbumDiscovery',
      useGatedHook: () =>
        useAlbumDiscovery({ albumTitle: 'Rumours', artist: 'Fleetwood Mac', enabled: true }),
      emptyResult: { tracks: [], isLoading: false, isError: false },
    },
    {
      hook: 'useArtistDiscovery',
      useGatedHook: () => useArtistDiscovery({ artistName: 'Fleetwood Mac', enabled: true }),
      emptyResult: {
        imageUrl: null,
        sources: [],
        isLoading: false,
        isError: false,
      },
    },
    {
      hook: 'useArtistContent',
      useGatedHook: () =>
        useArtistContent({ sources: [spotifyAlbum], artistName: 'Fleetwood Mac' }),
      emptyResult: {
        topTracks: [],
        albums: [],
        isLoading: false,
        isErrorAlbums: false,
      },
    },
    {
      hook: 'useAlbumTracks',
      useGatedHook: () => useAlbumTracks({ provider: 'spotify', externalId: 'album-1' }),
      emptyResult: { tracks: [], isLoading: false, isError: false },
    },
    {
      hook: 'useRelatedTracks',
      useGatedHook: () => useRelatedTracks({ sources: [soundcloudTrack] }),
      emptyResult: { relatedTracks: [], isLoading: false, isError: false },
    },
  ];

  const retryAffordances: { hook: string; useRetryAffordance: () => () => void }[] = [
    {
      hook: 'useAlbumTracks',
      useRetryAffordance: () =>
        useAlbumTracks({ provider: 'spotify', externalId: 'album-1' }).refetch,
    },
    {
      hook: 'useAlbumDiscovery',
      useRetryAffordance: () =>
        useAlbumDiscovery({ albumTitle: 'Rumours', artist: 'Fleetwood Mac', enabled: true })
          .refetch,
    },
    {
      hook: 'useArtistDiscovery',
      useRetryAffordance: () =>
        useArtistDiscovery({ artistName: 'Fleetwood Mac', enabled: true }).refetch,
    },
    {
      hook: 'useArtistContent',
      useRetryAffordance: () =>
        useArtistContent({ sources: [spotifyAlbum], artistName: 'Fleetwood Mac' }).refetch,
    },
  ];

  describe('detail fetches — remote kill switch', () => {
    it.each(gatedHooks)(
      '$hook fires no request and reports no data while the switch is off',
      async ({ useGatedHook, emptyResult }) => {
        switchDetailFetches(false);

        const { result } = renderHook(useGatedHook, {
          wrapper: createWrapper(createTestQueryClient()),
        });
        await settle();

        expect(__http.requests).toHaveLength(0);
        expect(result.current).toMatchObject(emptyResult);
      },
    );

    it.each(retryAffordances)(
      '$hook ignores a retry tapped while the switch is off',
      async ({ useRetryAffordance }) => {
        switchDetailFetches(false);
        const { result } = renderHook(useRetryAffordance, {
          wrapper: createWrapper(createTestQueryClient()),
        });
        await settle();

        act(() => result.current());
        await settle();

        expect(__http.requests).toHaveLength(0);
      },
    );

    it('fetches again when the switch is turned back on, without a remount', async () => {
      switchDetailFetches(false);
      renderHook(() => useAlbumTracks({ provider: 'spotify', externalId: 'album-1' }), {
        wrapper: createWrapper(createTestQueryClient()),
      });
      await settle();
      expect(__http.requests).toHaveLength(0);

      switchDetailFetches(true);

      await waitFor(() => expect(__http.countFor(ALBUM_TRACKS_PATH)).toBe(1));
    });

    it('leaves the fetches running while another loop is switched off', async () => {
      act(() => applyKillSwitches({ sse_enabled: false, offline_downloads_enabled: false }));

      renderHook(() => useAlbumTracks({ provider: 'spotify', externalId: 'album-1' }), {
        wrapper: createWrapper(createTestQueryClient()),
      });

      await waitFor(() => expect(__http.countFor(ALBUM_TRACKS_PATH)).toBe(1));
    });

    describe('source resolution and lateral navigation', () => {
      const unsourcedTrack = {
        kind: 'track' as const,
        title: 'Karma Police',
        subtitle: 'Radiohead',
        image_url: null,
        confidence: 'high' as const,
        sources: [],
        extras: {},
      };

      it('useResolveMissingSources sends no search while the switch is off, then does once it is on', async () => {
        switchDetailFetches(false);
        renderHook(() => useResolveMissingSources(unsourcedTrack), {
          wrapper: createWrapper(createTestQueryClient()),
        });
        await settle();
        expect(__http.requests).toHaveLength(0);

        switchDetailFetches(true);

        await waitFor(() => expect(__http.requests).toHaveLength(1));
      });

      it('useLateralNav shows an unavailable message and sends no search while the switch is off', async () => {
        switchDetailFetches(false);
        const { result } = renderHook(() => useLateralNav('/discover/detail'), {
          wrapper: createWrapper(createTestQueryClient()),
        });

        await act(() => result.current.navigateTo('Radiohead', 'artist'));

        expect(__http.requests).toHaveLength(0);
        expect(result.current.error).toBe('Search is temporarily unavailable');
        expect(result.current.state).toBe('idle');
      });

      it('useLateralNav searches again once the switch is back on, without a remount', async () => {
        switchDetailFetches(false);
        const { result } = renderHook(() => useLateralNav('/discover/detail'), {
          wrapper: createWrapper(createTestQueryClient()),
        });
        switchDetailFetches(true);

        await act(() => result.current.navigateTo('Radiohead', 'artist'));

        expect(__http.requests).toHaveLength(1);
      });
    });
  });
});

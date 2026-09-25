// Regression for issue #1666: every enrichment and discovery fetch on the detail screen must be
// gated by the remote kill switch, so a chatty provider integration can be stopped without a
// release.

import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react-native';

import { supabase } from '@shared/auth/supabaseClient';
import { createMemoryFileStore } from '@shared/files/__tests__/memoryFileStore';
import { applyKillSwitches, setKillSwitchFileStore } from '@shared/killSwitch/killSwitch';

import { useAlbumDiscovery } from '../hooks/useAlbumDiscovery';
import { useAlbumTracks } from '../hooks/useAlbumTracks';
import { useArtistContent } from '../hooks/useArtistContent';
import { useArtistDiscovery } from '../hooks/useArtistDiscovery';
import { useDeezerEnrichment } from '../hooks/useDeezerEnrichment';
import { useLateralNav } from '../hooks/useLateralNav';
import { useResolveMissingSources } from '../hooks/useResolveMissingSources';
import { useEnrichment } from '../hooks/useEnrichment';
import { useLastFmEnrichment } from '../hooks/useLastFmEnrichment';
import { useRelatedTracks } from '../hooks/useRelatedTracks';

const { __http } = require('../../../../jest/doubles/fetch.js');

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn() } },
}));

jest.mock('expo-router', () => ({
  useRouter: () => ({ push: jest.fn() }),
  useSegments: () => ['(tabs)', 'discover'],
}));

const ALBUM_TRACKS_PATH = 'GET /v1/discovery/albums/spotify/album-1/tracks';

const spotifyAlbum = { provider: 'spotify', external_id: 'album-1', url: 'https://s.example/1' };
const soundcloudTrack = {
  provider: 'soundcloud',
  external_id: 'track-1',
  url: 'https://s.example/t',
};

function createWrapper(queryClient: QueryClient) {
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
  };
}

function freshClient() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } });
}

function switchDetailFetches(enabled: boolean): void {
  act(() => applyKillSwitches({ detail_enrichment_enabled: enabled }));
}

// A disabled query never resolves a promise, so nothing waits: let React and react-query settle
// once, then assert that no request was made.
async function settle(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
  });
}

beforeEach(() => {
  (supabase.auth.getSession as jest.Mock).mockReset().mockResolvedValue({
    data: { session: { access_token: 'tok' } },
    error: null,
  });
  setKillSwitchFileStore(createMemoryFileStore());
  jest.spyOn(console, 'warn').mockImplementation(() => undefined);
  // Every endpoint answers, so a fetch that slips past the switch shows up as a request rather
  // than as an unrelated "no rule registered" failure.
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
    emptyResult: { enrichment: null, isLoading: false, isError: false },
  },
  {
    hook: 'useDeezerEnrichment',
    useGatedHook: () =>
      useDeezerEnrichment({ kind: 'album', title: 'Rumours', subtitle: 'Fleetwood Mac' }),
    emptyResult: { enrichment: null, isLoading: false, isError: false },
  },
  {
    hook: 'useLastFmEnrichment',
    useGatedHook: () =>
      useLastFmEnrichment({ kind: 'artist', title: 'Fleetwood Mac', subtitle: null }),
    emptyResult: { enrichment: null, isLoading: false, isError: false },
  },
  {
    hook: 'useAlbumDiscovery',
    useGatedHook: () =>
      useAlbumDiscovery({ albumTitle: 'Rumours', artist: 'Fleetwood Mac', enabled: true }),
    emptyResult: { albumResult: null, tracks: [], isLoading: false, isError: false },
  },
  {
    hook: 'useArtistDiscovery',
    useGatedHook: () => useArtistDiscovery({ artistName: 'Fleetwood Mac', enabled: true }),
    emptyResult: {
      artistResult: null,
      imageUrl: null,
      sources: [],
      isLoading: false,
      isError: false,
    },
  },
  {
    hook: 'useArtistContent',
    useGatedHook: () => useArtistContent({ sources: [spotifyAlbum], artistName: 'Fleetwood Mac' }),
    emptyResult: {
      topTracks: [],
      albums: [],
      isLoadingTracks: false,
      isLoadingAlbums: false,
      isErrorTracks: false,
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

// react-query's own `refetch` fetches whatever `enabled` says, so every retry affordance the
// screen exposes is its own way past the switch.
const retryAffordances: { hook: string; useRetryAffordance: () => () => void }[] = [
  {
    hook: 'useAlbumTracks',
    useRetryAffordance: () =>
      useAlbumTracks({ provider: 'spotify', externalId: 'album-1' }).refetch,
  },
  {
    hook: 'useAlbumDiscovery',
    useRetryAffordance: () =>
      useAlbumDiscovery({ albumTitle: 'Rumours', artist: 'Fleetwood Mac', enabled: true }).refetch,
  },
  {
    hook: 'useArtistDiscovery',
    useRetryAffordance: () =>
      useArtistDiscovery({ artistName: 'Fleetwood Mac', enabled: true }).refetch,
  },
  {
    hook: 'useArtistContent',
    useRetryAffordance: () =>
      useArtistContent({ sources: [spotifyAlbum], artistName: 'Fleetwood Mac' }).refetchTracks,
  },
];

describe('detail fetches — remote kill switch', () => {
  it.each(gatedHooks)(
    '$hook fires no request and reports no data while the switch is off',
    async ({ useGatedHook, emptyResult }) => {
      switchDetailFetches(false);

      const { result } = renderHook(useGatedHook, { wrapper: createWrapper(freshClient()) });
      await settle();

      expect(__http.requests).toHaveLength(0);
      expect(result.current).toMatchObject(emptyResult);
    },
  );

  it.each(retryAffordances)(
    '$hook ignores a retry tapped while the switch is off',
    async ({ useRetryAffordance }) => {
      switchDetailFetches(false);
      const { result } = renderHook(useRetryAffordance, { wrapper: createWrapper(freshClient()) });
      await settle();

      act(() => result.current());
      await settle();

      expect(__http.requests).toHaveLength(0);
    },
  );

  it('fetches again when the switch is turned back on, without a remount', async () => {
    switchDetailFetches(false);
    renderHook(() => useAlbumTracks({ provider: 'spotify', externalId: 'album-1' }), {
      wrapper: createWrapper(freshClient()),
    });
    await settle();
    expect(__http.requests).toHaveLength(0);

    switchDetailFetches(true);

    await waitFor(() => expect(__http.countFor(ALBUM_TRACKS_PATH)).toBe(1));
  });

  it('leaves the fetches running while another loop is switched off', async () => {
    act(() => applyKillSwitches({ sse_enabled: false, offline_downloads_enabled: false }));

    renderHook(() => useAlbumTracks({ provider: 'spotify', externalId: 'album-1' }), {
      wrapper: createWrapper(freshClient()),
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
        wrapper: createWrapper(freshClient()),
      });
      await settle();
      expect(__http.requests).toHaveLength(0);

      switchDetailFetches(true);

      await waitFor(() => expect(__http.requests).toHaveLength(1));
    });

    it('useLateralNav shows an unavailable message and sends no search while the switch is off', async () => {
      switchDetailFetches(false);
      const { result } = renderHook(() => useLateralNav(), {
        wrapper: createWrapper(freshClient()),
      });

      await act(() => result.current.navigateTo('Radiohead', 'artist'));

      expect(__http.requests).toHaveLength(0);
      expect(result.current.error).toBe('Search is temporarily unavailable');
      expect(result.current.state).toBe('idle');
    });

    it('useLateralNav searches again once the switch is back on, without a remount', async () => {
      switchDetailFetches(false);
      const { result } = renderHook(() => useLateralNav(), {
        wrapper: createWrapper(freshClient()),
      });
      switchDetailFetches(true);

      await act(() => result.current.navigateTo('Radiohead', 'artist'));

      expect(__http.requests).toHaveLength(1);
    });
  });
});

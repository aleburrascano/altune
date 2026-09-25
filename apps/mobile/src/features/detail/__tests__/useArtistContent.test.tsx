import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { renderHook, waitFor } from '@testing-library/react-native';
import type { ReactNode } from 'react';

import { supabase } from '@shared/auth/supabaseClient';
import type { DiscoverySource } from '@shared/api-client/discovery';
import { recordEvent } from '@shared/telemetry/recordEvent';

import { useArtistContent } from '../hooks/useArtistContent';
import { _resetDetailHealthForTest } from '../detailHealth';

const mockGetArtistContent = jest.fn();
jest.mock('@shared/api-client/enrichment', () => ({
  ...jest.requireActual('@shared/api-client/enrichment'),
  getArtistContent: (...args: unknown[]) => mockGetArtistContent(...args),
}));

jest.mock('@shared/telemetry/recordEvent', () => ({ recordEvent: jest.fn() }));

const { __http } = require('../../../../jest/doubles/fetch.js');

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn() } },
}));

const ARTIST_CONTENT_PATH = 'GET /v1/discovery/artists/spotify/artist-1/content';

const emptyContentFetch = {
  items: [],
  provider: 'spotify',
  status: 'ok',
  latency_ms: 3,
};

function createWrapper(queryClient: QueryClient) {
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
  };
}

function freshClient() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } });
}

beforeEach(() => {
  mockGetArtistContent.mockImplementation(
    jest.requireActual('@shared/api-client/enrichment').getArtistContent,
  );
  (supabase.auth.getSession as jest.Mock).mockReset().mockResolvedValue({
    data: { session: { access_token: 'tok' } },
    error: null,
  });
});

describe('useArtistContent bounds the artist albums fetch', () => {
  it('caps the albums request at the same limit as the sibling album track fetches', async () => {
    __http.reply(ARTIST_CONTENT_PATH, {
      status: 200,
      json: { top_tracks: emptyContentFetch, albums: emptyContentFetch },
    });
    const queryClient = freshClient();

    const { result } = renderHook(
      () =>
        useArtistContent({
          sources: [{ provider: 'spotify', external_id: 'artist-1', url: 'https://s.example/1' }],
        }),
      { wrapper: createWrapper(queryClient) },
    );

    await waitFor(() => expect(result.current.isLoadingAlbums).toBe(false));

    const params = new URLSearchParams(__http.last().query);
    expect(params.get('albums_limit')).toBe('100');
  });
});

describe('logging degraded content', () => {
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

  function contentResponse(overrides: {
    topStatus?: string;
    albumsStatus?: string;
  }): unknown {
    return {
      top_tracks: { items: [], provider: 'spotify', status: overrides.topStatus ?? 'ok', latency_ms: 1 },
      albums: { items: [], provider: 'spotify', status: overrides.albumsStatus ?? 'ok', latency_ms: 1 },
    };
  }

  describe('useArtistContent logs degraded per-provider content statuses', () => {
    it('logs which side/provider/artist failed when top_tracks status is not ok', async () => {
      mockGetArtistContent.mockResolvedValue(contentResponse({ topStatus: 'timeout' }));

      const { result } = renderHook(
        () =>
          useArtistContent({
            sources: [{ provider: 'spotify', external_id: 'artist-1', url: 'https://x' }],
            artistName: 'Radiohead',
          }),
        { wrapper: createWrapper(freshClient()) },
      );

      await waitFor(() => expect(result.current.isErrorTracks).toBe(true), { timeout: 5000 });

      expect(warnSpy).toHaveBeenCalledWith(
        '[detail] artist top_tracks fetch degraded',
        expect.objectContaining({
          status: 'timeout',
          provider: 'spotify',
          externalId: 'artist-1',
          artistName: 'Radiohead',
        }),
      );
    });

    it('logs when the whole content request throws', async () => {
      mockGetArtistContent.mockRejectedValue(new Error('network down'));

      const { result } = renderHook(
        () =>
          useArtistContent({
            sources: [{ provider: 'deezer', external_id: 'artist-2', url: 'https://x' }],
          }),
        { wrapper: createWrapper(freshClient()) },
      );

      await waitFor(() => expect(result.current.isErrorTracks).toBe(true), { timeout: 5000 });

      expect(warnSpy).toHaveBeenCalledWith(
        '[detail] artist content fetch failed',
        expect.objectContaining({
          provider: 'deezer',
          externalId: 'artist-2',
          error: 'network down',
        }),
      );
    });

    it('does not log when both statuses are ok', async () => {
      mockGetArtistContent.mockResolvedValue(contentResponse({}));

      const { result } = renderHook(
        () =>
          useArtistContent({
            sources: [{ provider: 'spotify', external_id: 'artist-3', url: 'https://x' }],
          }),
        { wrapper: createWrapper(freshClient()) },
      );

      await waitFor(() => expect(result.current.isLoadingTracks).toBe(false), { timeout: 5000 });

      expect(warnSpy).not.toHaveBeenCalled();
    });
  });
});

describe('aborting on unmount', () => {
  let client: QueryClient;

  function wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
  }

  beforeEach(() => {
    (supabase.auth.getSession as jest.Mock).mockResolvedValue({
      data: { session: { access_token: 'tok' } },
      error: null,
    });
    _resetDetailHealthForTest();
    (recordEvent as jest.Mock).mockReset().mockResolvedValue(undefined);
    client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  });

  afterEach(() => client.clear());

  describe.each([
    [
      'useArtistContent',
      'GET /v1/discovery/artists/spotify/a-1/content',
      (): void =>
        void useArtistContent({
          sources: [{ provider: 'spotify', external_id: 'a-1' } as DiscoverySource],
        }),
    ],
  ] as const)('%s, left before its request resolves', (_name, spec, useHook) => {
    it('aborts the in-flight request when the screen unmounts', async () => {
      __http.hang(spec);
      const { unmount } = renderHook(useHook, { wrapper });
      await waitFor(() => expect(__http.last()).toBeDefined());
      const { signal } = __http.last() as { signal: AbortSignal };
      expect(signal.aborted).toBe(false);

      unmount();

      await waitFor(() => expect(signal.aborted).toBe(true));
    });
  });
});

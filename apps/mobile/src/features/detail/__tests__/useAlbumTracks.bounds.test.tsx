import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { renderHook, waitFor } from '@testing-library/react-native';

import { supabase } from '@shared/auth/supabaseClient';

import { useAlbumTracks } from '../hooks/useAlbumTracks';
import { useAlbumDiscovery } from '../hooks/useAlbumDiscovery';

const { __http } = require('../../../../jest/doubles/fetch.js');

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn() } },
}));

const ALBUM_TRACKS_PATH = 'GET /v1/discovery/albums/spotify/album-1/tracks';

const emptyContent = {
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
  (supabase.auth.getSession as jest.Mock).mockReset().mockResolvedValue({
    data: { session: { access_token: 'tok' } },
    error: null,
  });
});

describe('useAlbumTracks bounds the album track fetch', () => {
  it('caps the request with an explicit limit instead of fetching an unbounded tracklist', async () => {
    __http.reply(ALBUM_TRACKS_PATH, { status: 200, json: emptyContent });
    const queryClient = freshClient();

    const { result } = renderHook(
      () => useAlbumTracks({ provider: 'spotify', externalId: 'album-1' }),
      { wrapper: createWrapper(queryClient) },
    );

    await waitFor(() => expect(result.current.isLoading).toBe(false));

    // The bug: the queryFn passed `undefined` for limit, so the server was asked
    // for the entire tracklist. The fix sends a bounded limit like the sibling
    // capped calls (artist albums/top-tracks, related tracks).
    const params = new URLSearchParams(__http.last().query);
    expect(params.get('limit')).toBe('100');
  });
});

describe('useAlbumTracks cancels in-flight requests when the screen unmounts', () => {
  it('aborts the request on navigate-away instead of letting it run to the timeout', async () => {
    __http.hang(ALBUM_TRACKS_PATH);
    const queryClient = freshClient();

    const { unmount } = renderHook(
      () => useAlbumTracks({ provider: 'spotify', externalId: 'album-1' }),
      { wrapper: createWrapper(queryClient) },
    );

    // Wait for the request to actually be dispatched so we can grab its signal.
    await waitFor(() => expect(__http.last()?.path).toBe('/v1/discovery/albums/spotify/album-1/tracks'));
    const { signal } = __http.last();
    expect(signal.aborted).toBe(false);

    // The bug: the queryFn never forwarded React Query's abort signal, so
    // unmounting left the request running (until the 15s deadline). The fix
    // threads the signal through, so navigating away aborts it immediately.
    unmount();

    await waitFor(() => expect(signal.aborted).toBe(true));
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

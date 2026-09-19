import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { renderHook, waitFor } from '@testing-library/react-native';

import { supabase } from '@shared/auth/supabaseClient';

import { useAlbumTracks } from '../hooks/useAlbumTracks';

const { __http } = require('../../../../jest/doubles/fetch.js');

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn() } },
}));

const ALBUM_TRACKS_PATH = 'GET /v1/discovery/albums/spotify/album-1/tracks';

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

describe('useAlbumTracks surfaces transient provider failures as errors', () => {
  // A transient outage returns an empty item list alongside a non-'ok' status.
  // The bug: only the literal 'error' status was treated as a failure, so
  // 'timeout' / 'rate_limited' / 'circuit_open' passed through as a "successful"
  // empty album and AlbumDetailBody rendered the false empty state.
  it.each(['timeout', 'rate_limited', 'circuit_open', 'error'] as const)(
    'flags isError and keeps tracks empty for status %s',
    async (status) => {
      __http.reply(ALBUM_TRACKS_PATH, {
        status: 200,
        json: { items: [], provider_name: 'spotify', status },
      });
      const queryClient = freshClient();

      const { result } = renderHook(
        () => useAlbumTracks({ provider: 'spotify', externalId: 'album-1' }),
        { wrapper: createWrapper(queryClient) },
      );

      await waitFor(() => expect(result.current.isLoading).toBe(false));

      expect(result.current.tracks).toEqual([]);
      expect(result.current.isError).toBe(true);
    },
  );

  it('does not flag isError for a genuinely empty but healthy album', async () => {
    __http.reply(ALBUM_TRACKS_PATH, {
      status: 200,
      json: { items: [], provider_name: 'spotify', status: 'ok' },
    });
    const queryClient = freshClient();

    const { result } = renderHook(
      () => useAlbumTracks({ provider: 'spotify', externalId: 'album-1' }),
      { wrapper: createWrapper(queryClient) },
    );

    await waitFor(() => expect(result.current.isLoading).toBe(false));

    expect(result.current.tracks).toEqual([]);
    expect(result.current.isError).toBe(false);
  });
});

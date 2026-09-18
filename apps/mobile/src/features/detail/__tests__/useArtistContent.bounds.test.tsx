import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { renderHook, waitFor } from '@testing-library/react-native';

import { supabase } from '@shared/auth/supabaseClient';

import { useArtistContent } from '../hooks/useArtistContent';

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

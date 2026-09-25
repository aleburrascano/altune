import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { renderHook, waitFor } from '@testing-library/react-native';

import { supabase } from '@shared/auth/supabaseClient';

import {
  LOOKUP_LIMIT,
  LOOKUP_MAX_PAGES,
  useLibraryTracksForAlbum,
} from '../hooks/useLibraryTracks';

const { __http } = require('../../../../jest/doubles/fetch.js');

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn() } },
}));

const TRACKS = 'GET /v1/tracks';

function row(i: number, album: string) {
  return {
    id: `t${i}`,
    title: `T${i}`,
    artist: 'Artist',
    album,
    duration_seconds: null,
    added_at: '2024-01-01T00:00:00Z',
    acquisition_status: 'ready',
    artwork_url: null,
    failure_reason: null,
    year: null,
    genre: null,
    track_number: null,
    album_artist: 'Artist',
    isrc: null,
    audio_ref: null,
  };
}

function page(rows: unknown[], hasMore: boolean) {
  const body = { items: rows, total: 0, limit: LOOKUP_LIMIT, offset: 0, has_more: hasMore };
  return { status: 200, json: body };
}

function filler(count: number) {
  return Array.from({ length: count }, (_, i) => row(i, 'Other'));
}

let client: QueryClient;

function wrapper({ children }: { children: React.ReactNode }) {
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

beforeEach(() => {
  __http.reset();
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  (supabase.auth.getSession as jest.Mock).mockReset().mockResolvedValue({
    data: { session: { access_token: 'tok' } },
    error: null,
  });
});

describe('useLibraryTracksForAlbum pages the lookup', () => {
  it('finds album tracks that sit past the first page', async () => {
    __http.replyOnce(TRACKS, page(filler(LOOKUP_LIMIT), true));
    __http.replyOnce(TRACKS, page([row(900, 'Hits')], false));
    const hook = renderHook(() => useLibraryTracksForAlbum('Hits', 'Artist'), { wrapper });
    await waitFor(() => expect(hook.result.current).toHaveLength(1));
    expect(hook.result.current.complete).toBe(true);
  });

  it('stops at the page ceiling and reports the lookup incomplete', async () => {
    __http.reply(TRACKS, page(filler(LOOKUP_LIMIT), true));
    const hook = renderHook(() => useLibraryTracksForAlbum('Hits', 'Artist'), { wrapper });
    await waitFor(() => expect(__http.countFor(TRACKS)).toBe(LOOKUP_MAX_PAGES));
    await waitFor(() => expect(hook.result.current.complete).toBe(false));
  });
});

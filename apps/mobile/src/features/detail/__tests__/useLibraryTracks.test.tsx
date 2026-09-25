import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { renderHook, waitFor } from '@testing-library/react-native';
import type { ReactNode } from 'react';

import { supabase } from '@shared/auth/supabaseClient';

import {
  LOOKUP_LIMIT,
  LOOKUP_MAX_PAGES,
  useLibraryTracksForAlbum,
  useLibraryTracksForArtist,
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

describe('aborting on unmount', () => {
  // #1667: leaving a detail screen must abort its in-flight library lookup instead of
  // letting it run to the shared 15s deadline. TanStack only aborts on unmount when the
  // queryFn consumed the context signal, so this drives the real api-client against the
  // fetch double and checks the recorded request's signal.

  let client: QueryClient;

  function wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
  }

  beforeEach(() => {
    (supabase.auth.getSession as jest.Mock).mockResolvedValue({
      data: { session: { access_token: 'tok' } },
      error: null,
    });
    client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  });

  afterEach(() => client.clear());

  describe.each([
    [
      'useLibraryTracksForAlbum',
      'GET /v1/tracks',
      (): void => void useLibraryTracksForAlbum('discovery', 'daft punk'),
    ],
    [
      'useLibraryTracksForArtist',
      'GET /v1/tracks',
      (): void => void useLibraryTracksForArtist('daft punk'),
    ],
  ] as const)('%s, left before its request resolves', (_name, spec, useHook) => {
    it('aborts the in-flight request when the screen unmounts', async () => {
      __http.hang(spec);
      const { unmount } = renderHook(useHook, { wrapper });
      await waitFor(() => expect(__http.last()).toBeDefined());
      const { signal } = __http.last() as { signal: AbortSignal };

      unmount();

      await waitFor(() => expect(signal.aborted).toBe(true));
    });
  });
});

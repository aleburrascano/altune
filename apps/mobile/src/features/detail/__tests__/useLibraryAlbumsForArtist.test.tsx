// #1668: the one detail list that asked the library for albums without stating a
// bound, leaving the row count to a server default the client cannot see.

import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { renderHook, waitFor } from '@testing-library/react-native';
import type { ReactNode } from 'react';

import { supabase } from '@shared/auth/supabaseClient';
import { libraryKeys } from '@shared/lib/query-keys';

import { useLibraryAlbumsForArtist } from '../hooks/useLibraryAlbumsForArtist';

const { __http } = require('../../../../jest/doubles/fetch.js');

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn() } },
}));

const LIBRARY_ALBUMS_PATH = 'GET /v1/library/albums';

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

describe('useLibraryAlbumsForArtist bounds the library albums fetch', () => {
  it('caps the request at the same limit as the sibling detail lists', async () => {
    __http.reply(LIBRARY_ALBUMS_PATH, { status: 200, json: { items: [], total: 0 } });
    const queryClient = freshClient();

    renderHook(() => useLibraryAlbumsForArtist('daft punk', true), {
      wrapper: createWrapper(queryClient),
    });

    await waitFor(() => expect(__http.last()?.path).toBe('/v1/library/albums'));

    const params = new URLSearchParams(__http.last().query);
    expect(params.get('limit')).toBe('100');
  });

  it('caches under a key the library screen’s uncapped albums query cannot satisfy', async () => {
    __http.reply(LIBRARY_ALBUMS_PATH, { status: 200, json: { items: [], total: 0 } });
    const queryClient = freshClient();
    // What the library home screen stores for the same artist name and sort: the
    // server's default page, which is shorter than this list asked for.
    queryClient.setQueryData(libraryKeys.albums('daft punk', 'recent'), { items: [], total: 0 });

    renderHook(() => useLibraryAlbumsForArtist('daft punk', true), {
      wrapper: createWrapper(queryClient),
    });

    await waitFor(() => expect(__http.countFor(LIBRARY_ALBUMS_PATH)).toBe(1));
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
      'useLibraryAlbumsForArtist',
      'GET /v1/library/albums',
      (): void => void useLibraryAlbumsForArtist('daft punk', true),
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

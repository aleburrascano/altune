import type { QueryClient } from '@tanstack/react-query';
import { renderHook, waitFor } from '@testing-library/react-native';

import { supabase } from '@shared/auth/supabaseClient';
import { libraryKeys } from '@shared/lib/query-keys';

import { useLibraryAlbumsForArtist } from '../hooks/useLibraryAlbumsForArtist';
import { createTestQueryClient, createWrapper, mockSupabaseSession } from './support/queryHarness';

const { __http } = require('../../../../jest/doubles/fetch.js');

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn() } },
}));

const LIBRARY_ALBUMS_PATH = 'GET /v1/library/albums';

beforeEach(() => {
  (supabase.auth.getSession as jest.Mock).mockReset().mockResolvedValue(mockSupabaseSession());
});

describe('useLibraryAlbumsForArtist bounds the library albums fetch', () => {
  it('caps the request at the same limit as the sibling detail lists', async () => {
    __http.reply(LIBRARY_ALBUMS_PATH, { status: 200, json: { items: [], total: 0 } });
    const queryClient = createTestQueryClient();

    renderHook(() => useLibraryAlbumsForArtist('daft punk', true), {
      wrapper: createWrapper(queryClient),
    });

    await waitFor(() => expect(__http.last()?.path).toBe('/v1/library/albums'));

    const params = new URLSearchParams(__http.last().query);
    expect(params.get('limit')).toBe('100');
  });

  it('caches under a key the library screen’s uncapped albums query cannot satisfy', async () => {
    __http.reply(LIBRARY_ALBUMS_PATH, { status: 200, json: { items: [], total: 0 } });
    const queryClient = createTestQueryClient();
    queryClient.setQueryData(libraryKeys.albums('daft punk', 'recent'), { items: [], total: 0 });

    renderHook(() => useLibraryAlbumsForArtist('daft punk', true), {
      wrapper: createWrapper(queryClient),
    });

    await waitFor(() => expect(__http.countFor(LIBRARY_ALBUMS_PATH)).toBe(1));
  });
});

describe('aborting on unmount', () => {
  let client: QueryClient;

  let wrapper: ReturnType<typeof createWrapper>;

  beforeEach(() => {
    (supabase.auth.getSession as jest.Mock).mockResolvedValue(mockSupabaseSession());
    client = createTestQueryClient();
    wrapper = createWrapper(client);
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

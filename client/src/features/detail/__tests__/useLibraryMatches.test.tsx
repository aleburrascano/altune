import type { QueryClient } from '@tanstack/react-query';

import { renderHook, waitFor } from '@testing-library/react-native';
import { libraryKeys } from '@shared/query/queryKeys';

import {
  LOOKUP_LIMIT,
  LOOKUP_MAX_PAGES,
  useLibraryTracksForAlbum,
  useLibraryTracksForArtist,
  useLibraryAlbumsForArtist,
} from '@/features/detail/hooks/useLibraryMatches';
import {
  createTestQueryClient,
  createWrapper,
  signInTestUser,
} from '@/features/detail/__tests__/support/queryHarness';

const { __http } = require('../../../../jest/doubles/fetch.js');

describe('useLibraryTracks', () => {
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

  let wrapper: ReturnType<typeof createWrapper>;

  beforeEach(async () => {
    __http.reset();
    client = createTestQueryClient();
    wrapper = createWrapper(client);
    await signInTestUser();
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
    let client: QueryClient;

    let wrapper: ReturnType<typeof createWrapper>;

    beforeEach(async () => {
      await signInTestUser();
      client = createTestQueryClient();
      wrapper = createWrapper(client);
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
        await waitFor(() => expect(__http.countFor(spec)).toBeGreaterThan(0));
        const { signal } = __http.last() as { signal: AbortSignal };

        unmount();

        await waitFor(() => expect(signal.aborted).toBe(true));
      });
    });
  });

  describe('a lookup that cannot be trusted as the whole library', () => {
    it('reports the album lookup incomplete when the request fails', async () => {
      __http.fail(TRACKS);
      const hook = renderHook(() => useLibraryTracksForAlbum('Hits', 'Artist'), { wrapper });
      await waitFor(() => expect(__http.countFor(TRACKS)).toBeGreaterThan(0));
      await waitFor(() => expect(hook.result.current.complete).toBe(false));
    });

    it('reports the artist lookup incomplete at the page ceiling', async () => {
      __http.reply(TRACKS, page(filler(LOOKUP_LIMIT), true));
      const hook = renderHook(() => useLibraryTracksForArtist('Artist'), { wrapper });
      await waitFor(() => expect(__http.countFor(TRACKS)).toBe(LOOKUP_MAX_PAGES));
      await waitFor(() => expect(hook.result.current.complete).toBe(false));
    });
  });
});

describe('useLibraryAlbumsForArtist', () => {
  const LIBRARY_ALBUMS_PATH = 'GET /v1/library/albums';

  beforeEach(async () => {
    await signInTestUser();
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

    beforeEach(async () => {
      await signInTestUser();
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
        await waitFor(() => expect(__http.countFor(spec)).toBeGreaterThan(0));
        const { signal } = __http.last() as { signal: AbortSignal };

        unmount();

        await waitFor(() => expect(signal.aborted).toBe(true));
      });
    });
  });
});

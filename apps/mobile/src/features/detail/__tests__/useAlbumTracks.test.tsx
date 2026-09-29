import { renderHook, waitFor } from '@testing-library/react-native';

import { supabase } from '@shared/auth/supabaseClient';

import { useAlbumTracks } from '../hooks/useAlbumTracks';
import { createTestQueryClient, createWrapper, mockSupabaseSession } from './support/queryHarness';

const { __http } = require('../../../../jest/doubles/fetch.js');

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn() } },
}));

const ALBUM_TRACKS_PATH = 'GET /v1/discovery/albums/spotify/album-1/tracks';

beforeEach(() => {
  (supabase.auth.getSession as jest.Mock).mockReset().mockResolvedValue(mockSupabaseSession());
});

const emptyContent = {
  items: [],
  provider: 'spotify',
  status: 'ok',
  latency_ms: 3,
};

describe('useAlbumTracks bounds the album track fetch', () => {
  it('caps the request with an explicit limit instead of fetching an unbounded tracklist', async () => {
    __http.reply(ALBUM_TRACKS_PATH, { status: 200, json: emptyContent });
    const queryClient = createTestQueryClient();

    const { result } = renderHook(
      () => useAlbumTracks({ provider: 'spotify', externalId: 'album-1' }),
      { wrapper: createWrapper(queryClient) },
    );

    await waitFor(() => expect(result.current.isLoading).toBe(false));

    const params = new URLSearchParams(__http.last().query);
    expect(params.get('limit')).toBe('100');
  });
});

describe('useAlbumTracks cancels in-flight requests when the screen unmounts', () => {
  it('aborts the request on navigate-away instead of letting it run to the timeout', async () => {
    __http.hang(ALBUM_TRACKS_PATH);
    const queryClient = createTestQueryClient();

    const { unmount } = renderHook(
      () => useAlbumTracks({ provider: 'spotify', externalId: 'album-1' }),
      { wrapper: createWrapper(queryClient) },
    );

    await waitFor(() =>
      expect(__http.last()?.path).toBe('/v1/discovery/albums/spotify/album-1/tracks'),
    );
    const { signal } = __http.last();
    expect(signal.aborted).toBe(false);

    unmount();

    await waitFor(() => expect(signal.aborted).toBe(true));
  });
});

describe('useAlbumTracks surfaces transient provider failures as errors', () => {
  it.each(['timeout', 'rate_limited', 'circuit_open', 'error'] as const)(
    'flags isError and keeps tracks empty for status %s',
    async (status) => {
      __http.reply(ALBUM_TRACKS_PATH, {
        status: 200,
        json: { items: [], provider_name: 'spotify', status },
      });
      const queryClient = createTestQueryClient();

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
    const queryClient = createTestQueryClient();

    const { result } = renderHook(
      () => useAlbumTracks({ provider: 'spotify', externalId: 'album-1' }),
      { wrapper: createWrapper(queryClient) },
    );

    await waitFor(() => expect(result.current.isLoading).toBe(false));

    expect(result.current.tracks).toEqual([]);
    expect(result.current.isError).toBe(false);
  });
});

describe('useAlbumTracks reopened against the 30-minute content cache window', () => {
  const CACHED_ALBUM_PATH = 'GET /v1/discovery/albums/spotify/album-cache/tracks';
  const { act } = require('@testing-library/react-native');

  it.each([
    ['serves the cached tracklist without a request 1 ms before 30 minutes', 1_799_999, 1],
    ['asks the server again 1 ms after 30 minutes', 1_800_001, 2],
  ])('%s', async (_behaviour, elapsedMs, expectedRequests) => {
    let now = 1_700_000_000_000;
    jest.spyOn(Date, 'now').mockImplementation(() => now);
    __http.reply(CACHED_ALBUM_PATH, {
      status: 200,
      json: { items: [], provider_name: 'spotify', status: 'ok' },
    });
    const wrapper = createWrapper(createTestQueryClient());
    const useHook = () => useAlbumTracks({ provider: 'spotify', externalId: 'album-cache' });

    const first = renderHook(useHook, { wrapper });
    await waitFor(() => expect(first.result.current.isLoading).toBe(false));
    expect(first.result.current.isError).toBe(false);
    first.unmount();
    now += elapsedMs;
    renderHook(useHook, { wrapper });
    await act(async () => {
      await new Promise((resolve) => setImmediate(resolve));
    });

    expect(__http.countFor(CACHED_ALBUM_PATH)).toBe(expectedRequests);
  });
});

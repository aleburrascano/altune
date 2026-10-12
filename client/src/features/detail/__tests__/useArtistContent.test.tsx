import type { QueryClient } from '@tanstack/react-query';
import { renderHook, waitFor } from '@testing-library/react-native';

import type { DiscoverySource } from '@shared/apiClient/discovery';
import type { ArtistContentResponse } from '@shared/apiClient/enrichment';

import { useArtistContent } from '@/features/detail/hooks/useArtistContent';
import { _resetDetailHealthForTest } from '@/features/detail/domain/detailHealth';
import { DetailPortProvider } from '@/features/detail/ports/DetailPortProvider';
import type { DetailPort } from '@/features/detail/ports/detailPort';
import { createMemoryDetailPort } from '@/features/detail/__tests__/memoryDetailPort';
import {
  createTestQueryClient,
  createWrapper,
  signInTestUser,
} from '@/features/detail/__tests__/support/queryHarness';

const { __http } = require('../../../../jest/doubles/fetch.js');

const ARTIST_CONTENT_PATH = 'GET /v1/discovery/artists/spotify/artist-1/content';

function createPortWrapper(port: DetailPort) {
  const Base = createWrapper(createTestQueryClient());
  return function PortWrapper({ children }: { children: React.ReactNode }) {
    return (
      <Base>
        <DetailPortProvider port={port}>{children}</DetailPortProvider>
      </Base>
    );
  };
}

const emptyContentFetch = {
  items: [],
  provider: 'spotify',
  status: 'ok',
  latency_ms: 3,
};

beforeEach(async () => {
  await signInTestUser();
});

describe('useArtistContent bounds the artist albums fetch', () => {
  it('caps the albums request at the same limit as the sibling album track fetches', async () => {
    __http.reply(ARTIST_CONTENT_PATH, {
      status: 200,
      json: { top_tracks: emptyContentFetch, albums: emptyContentFetch },
    });
    const queryClient = createTestQueryClient();

    const { result } = renderHook(
      () =>
        useArtistContent({
          sources: [{ provider: 'spotify', external_id: 'artist-1', url: 'https://s.example/1' }],
        }),
      { wrapper: createWrapper(queryClient) },
    );

    await waitFor(() => expect(result.current.isLoading).toBe(false));

    const params = new URLSearchParams(__http.last().query);
    expect(params.get('albums_limit')).toBe('100');
  });
});

describe('useArtistContent sends the ids the search card knows', () => {
  it('asks with the first source and lists every source', async () => {
    __http.reply(ARTIST_CONTENT_PATH, {
      status: 200,
      json: { top_tracks: emptyContentFetch, albums: emptyContentFetch },
    });

    const { result } = renderHook(
      () =>
        useArtistContent({
          sources: [
            { provider: 'spotify', external_id: 'artist-1', url: '' },
            { provider: 'applemusic', external_id: '42', url: '' },
          ],
        }),
      { wrapper: createWrapper(createTestQueryClient()) },
    );

    await waitFor(() => expect(result.current.isLoading).toBe(false));

    const params = new URLSearchParams(__http.last().query);
    expect(params.get('ids')).toBe('spotify:artist-1,applemusic:42');
  });
});

describe('logging degraded content', () => {
  let warnSpy: jest.SpyInstance;

  beforeEach(() => {
    jest.clearAllMocks();
    warnSpy = jest.spyOn(console, 'warn').mockImplementation(() => {});
  });

  afterEach(() => {
    warnSpy.mockRestore();
  });

  function contentResponse(overrides: { topStatus?: string; albumsStatus?: string }): unknown {
    return {
      top_tracks: {
        items: [],
        provider: 'spotify',
        status: overrides.topStatus ?? 'ok',
        latency_ms: 1,
      },
      albums: {
        items: [],
        provider: 'spotify',
        status: overrides.albumsStatus ?? 'ok',
        latency_ms: 1,
      },
    };
  }

  describe('useArtistContent logs degraded per-provider content statuses', () => {
    it('logs which side/provider/artist failed when top_tracks status is not ok', async () => {
      const port = createMemoryDetailPort({
        artistContent: {
          'artist-1': contentResponse({ topStatus: 'timeout' }) as ArtistContentResponse,
        },
      });

      const { result } = renderHook(
        () =>
          useArtistContent({
            sources: [{ provider: 'spotify', external_id: 'artist-1', url: 'https://x' }],
          }),
        { wrapper: createPortWrapper(port) },
      );

      await waitFor(() => expect(result.current.tracksFailure).not.toBeNull(), { timeout: 5000 });

      expect(warnSpy).toHaveBeenCalledWith(
        '[detail] artist top_tracks fetch degraded',
        expect.objectContaining({
          status: 'timeout',
          provider: 'spotify',
          externalId: 'artist-1',
        }),
      );
    });

    it('logs when the whole content request throws', async () => {
      const port: DetailPort = {
        ...createMemoryDetailPort(),
        getArtistContent: () => Promise.reject(new Error('network down')),
      };

      const { result } = renderHook(
        () =>
          useArtistContent({
            sources: [{ provider: 'deezer', external_id: 'artist-2', url: 'https://x' }],
          }),
        { wrapper: createPortWrapper(port) },
      );

      await waitFor(() => expect(result.current.tracksFailure).not.toBeNull(), { timeout: 5000 });

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
      const port = createMemoryDetailPort({
        artistContent: { 'artist-3': contentResponse({}) as ArtistContentResponse },
      });

      const { result } = renderHook(
        () =>
          useArtistContent({
            sources: [{ provider: 'spotify', external_id: 'artist-3', url: 'https://x' }],
          }),
        { wrapper: createPortWrapper(port) },
      );

      await waitFor(() => expect(result.current.isLoading).toBe(false), { timeout: 5000 });

      expect(warnSpy).not.toHaveBeenCalled();
    });
  });
});

describe('aborting on unmount', () => {
  let client: QueryClient;

  let wrapper: ReturnType<typeof createWrapper>;

  beforeEach(async () => {
    await signInTestUser();
    _resetDetailHealthForTest();
    client = createTestQueryClient();
    wrapper = createWrapper(client);
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

describe('useArtistContent reopened against the 30-minute content cache window', () => {
  const CACHED_ARTIST_PATH = 'GET /v1/discovery/artists/spotify/artist-cache/content';
  const { act } = require('@testing-library/react-native');
  const okList = { items: [], provider_name: 'spotify', status: 'ok' };

  it.each([
    ['serves the cached artist page without a request 1 ms before 30 minutes', 1_799_999, 1],
    ['asks the server again 1 ms after 30 minutes', 1_800_001, 2],
  ])('%s', async (_behaviour, elapsedMs, expectedRequests) => {
    let now = 1_700_000_000_000;
    jest.spyOn(Date, 'now').mockImplementation(() => now);
    __http.reply(CACHED_ARTIST_PATH, {
      status: 200,
      json: { top_tracks: okList, albums: okList },
    });
    const wrapper = createWrapper(createTestQueryClient());
    const useHook = () =>
      useArtistContent({
        sources: [{ provider: 'spotify', external_id: 'artist-cache', url: 'https://s.example/c' }],
      });

    const first = renderHook(useHook, { wrapper });
    await waitFor(() => expect(first.result.current.isLoading).toBe(false));
    expect(first.result.current.tracksFailure).toBeNull();
    expect(first.result.current.albumsFailure).toBeNull();
    first.unmount();
    now += elapsedMs;
    renderHook(useHook, { wrapper });
    await act(async () => {
      await new Promise((resolve) => setImmediate(resolve));
    });

    expect(__http.countFor(CACHED_ARTIST_PATH)).toBe(expectedRequests);
  });
});

type ArtistContentProps = { sources: DiscoverySource[] };

describe('useArtistContent through the detail port', () => {
  const track = (title: string) =>
    ({ title }) as unknown as ArtistContentResponse['top_tracks']['items'][number];
  const served = (title: string): ArtistContentResponse => ({
    top_tracks: { items: [track(title)], provider_name: 'memory', status: 'ok' },
    albums: { items: [], provider_name: 'memory', status: 'ok' },
  });
  const radiohead = [{ provider: 'spotify', external_id: 'radiohead', url: 'https://x' }];
  const bjork = [{ provider: 'spotify', external_id: 'bjork', url: 'https://x' }];
  const unknown = [{ provider: 'spotify', external_id: 'unknown', url: 'https://x' }];
  const port = createMemoryDetailPort({
    artistContent: { radiohead: served('Creep'), bjork: served('Joga') },
  });

  it('returns content seeded for the artist', async () => {
    const { result } = renderHook(() => useArtistContent({ sources: radiohead }), {
      wrapper: createPortWrapper(port),
    });

    await waitFor(() => expect(result.current.topTracks).toHaveLength(1));
    expect(result.current.topTracks[0]).toMatchObject({ title: 'Creep' });
  });

  it('returns no content for an unseeded artist', async () => {
    const { result } = renderHook(() => useArtistContent({ sources: unknown }), {
      wrapper: createPortWrapper(port),
    });

    await waitFor(() => expect(result.current.isLoading).toBe(false));
    expect(result.current.topTracks).toEqual([]);
    expect(result.current.albums).toEqual([]);
  });

  it('returns the new artist content, not the previous one, after a rerender', async () => {
    const { result, rerender } = renderHook(
      ({ sources }: ArtistContentProps) => useArtistContent({ sources }),
      { wrapper: createPortWrapper(port), initialProps: { sources: radiohead } },
    );
    await waitFor(() => expect(result.current.topTracks[0]).toMatchObject({ title: 'Creep' }));

    rerender({ sources: bjork });

    await waitFor(() => expect(result.current.topTracks[0]).toMatchObject({ title: 'Joga' }));
  });
});

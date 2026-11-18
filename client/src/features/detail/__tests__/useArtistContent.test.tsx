import type { QueryClient } from '@tanstack/react-query';
import { renderHook, waitFor } from '@testing-library/react-native';

import type { ArtistContentResponse } from '@shared/apiClient';
import type { DiscoverySource } from '@shared/catalog';
import { detailKeys } from '@shared/query';
import {
  useArtistContent,
  _resetDetailHealthForTest,
  DetailPortProvider,
  createMemoryDetailPort,
  flushDetailHealth,
} from '@features/detail';
import type { DetailPort } from '@features/detail';
import { createTestQueryClient, createWrapper, signInTestUser } from '@testing/queryHarness';
import { freezeDate } from '@testing/freezeDate';

const CLOCK_START = 1_700_000_000_000;

const { __http } = require('@testing/doubles/fetch.js');

const ARTIST_CONTENT_PATH = 'GET /v1/discovery/artists/spotify/artist-1/content';

const servedList = { items: [], provider_name: 'spotify', status: 'ok' };

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

afterEach(() => {
  jest.useRealTimers();
});

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

      expect(port.warnings).toContainEqual([
        '[detail] artist top_tracks fetch degraded',
        expect.objectContaining({
          status: 'timeout',
          provider: 'spotify',
          externalId: 'artist-1',
        }),
      ]);
    });

    it('logs when the whole content request throws', async () => {
      const port = createMemoryDetailPort();
      const failing: DetailPort = {
        ...port,
        getArtistContent: () => Promise.reject(new Error('network down')),
      };

      const { result } = renderHook(
        () =>
          useArtistContent({
            sources: [{ provider: 'deezer', external_id: 'artist-2', url: 'https://x' }],
          }),
        { wrapper: createPortWrapper(failing) },
      );

      await waitFor(() => expect(result.current.tracksFailure).not.toBeNull(), { timeout: 5000 });

      expect(port.warnings).toContainEqual([
        '[detail] artist content fetch failed',
        expect.objectContaining({
          provider: 'deezer',
          externalId: 'artist-2',
          error: 'network down',
        }),
      ]);
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

      expect(port.warnings).toEqual([]);
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
      await waitFor(() => expect(__http.countFor(spec)).toBeGreaterThan(0));
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
    const clock = freezeDate(CLOCK_START);
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
    clock.advance(elapsedMs);
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

describe('useArtistContent tallies each fetch for the detail health metric', () => {
  const EVENTS = 'POST /v1/discovery/events';
  const spotifyArtist = [{ provider: 'spotify', external_id: 'artist-1', url: '' }];

  function sentHealth(): Record<string, unknown>[] {
    return (__http.requests as { method: string; path: string; body: string }[])
      .filter((request) => request.method === 'POST' && request.path === '/v1/discovery/events')
      .map(
        (request) => JSON.parse(request.body) as { type: string; payload: Record<string, unknown> },
      )
      .filter((event) => event.type === 'detail_health')
      .map((event) => event.payload);
  }

  async function renderAndFlush(): Promise<void> {
    const { result } = renderHook(() => useArtistContent({ sources: spotifyArtist }), {
      wrapper: createWrapper(createTestQueryClient()),
    });
    await waitFor(() => expect(result.current.isLoading).toBe(false));
    flushDetailHealth();
  }

  beforeEach(() => {
    _resetDetailHealthForTest();
    __http.reply(EVENTS, { status: 204 });
  });

  it('counts a fully served artist page as a success', async () => {
    __http.reply(ARTIST_CONTENT_PATH, {
      status: 200,
      json: { top_tracks: servedList, albums: servedList },
    });

    await renderAndFlush();

    await waitFor(() =>
      expect(sentHealth()).toEqual([
        expect.objectContaining({ content_artist_content_ok: 1, content_artist_content_failed: 0 }),
      ]),
    );
  });

  it('counts a request the provider refused as a failure', async () => {
    __http.reply(ARTIST_CONTENT_PATH, {
      status: 502,
      json: { status: 'error', code: 'discovery.provider_error' },
    });

    await renderAndFlush();

    await waitFor(() =>
      expect(sentHealth()).toEqual([
        expect.objectContaining({ content_artist_content_ok: 0, content_artist_content_failed: 1 }),
      ]),
    );
  });
});

describe('useArtistContent albums', () => {
  const spotifyArtist = [{ provider: 'spotify', external_id: 'artist-1', url: '' }];
  const album = {
    kind: 'album',
    title: 'Kid A',
    subtitle: 'Radiohead',
    image_url: null,
    confidence: 'high',
    sources: [{ provider: 'spotify', external_id: 'kid-a', url: '' }],
    extras: {},
  };

  function replyAlbums(status: string): void {
    __http.reply(ARTIST_CONTENT_PATH, {
      status: 200,
      json: {
        top_tracks: servedList,
        albums: { ...servedList, items: [album], status },
      },
    });
  }

  it('shows the albums of a served page', async () => {
    replyAlbums('ok');

    const { result } = renderHook(() => useArtistContent({ sources: spotifyArtist }), {
      wrapper: createWrapper(createTestQueryClient()),
    });

    await waitFor(() => expect(result.current.albums).toHaveLength(1));
    expect(result.current.albums[0]).toMatchObject({ title: 'Kid A' });
    expect(result.current.isErrorAlbums).toBe(false);
  });

  it('shows no albums and an albums error when the albums side is degraded', async () => {
    replyAlbums('timeout');

    const { result } = renderHook(() => useArtistContent({ sources: spotifyArtist }), {
      wrapper: createWrapper(createTestQueryClient()),
    });

    await waitFor(() => expect(result.current.isErrorAlbums).toBe(true));
    expect(result.current.albums).toEqual([]);
  });
});

describe('useArtistContent with no source to ask', () => {
  const { act } = require('@testing-library/react-native');

  it('sends nothing and reports no failure', async () => {
    const queryClient = createTestQueryClient();

    const { result } = renderHook(() => useArtistContent({ sources: [] }), {
      wrapper: createWrapper(queryClient),
    });
    await act(async () => {
      await new Promise((resolve) => setImmediate(resolve));
    });

    expect(__http.requests).toHaveLength(0);
    expect(result.current.tracksFailure).toBeNull();
    expect(result.current.albumsFailure).toBeNull();
    expect(queryClient.getQueryState(detailKeys.artistContent('', '', ''))).toBeDefined();
  });
});

describe('useArtistContent caches per artist', () => {
  it('keys the cache by the first source and every known id', async () => {
    __http.reply(ARTIST_CONTENT_PATH, {
      status: 200,
      json: { top_tracks: servedList, albums: servedList },
    });
    const queryClient = createTestQueryClient();
    const sources = [
      { provider: 'spotify', external_id: 'artist-1', url: '' },
      { provider: 'applemusic', external_id: '42', url: '' },
    ];

    const { result } = renderHook(() => useArtistContent({ sources }), {
      wrapper: createWrapper(queryClient),
    });
    await waitFor(() => expect(result.current.isLoading).toBe(false));

    const key = detailKeys.artistContent('spotify', 'artist-1', 'spotify:artist-1,applemusic:42');
    expect(queryClient.getQueryState(key)?.status).toBe('success');
  });
});

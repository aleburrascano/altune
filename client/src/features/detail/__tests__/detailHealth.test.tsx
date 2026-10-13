import type { QueryClient } from '@tanstack/react-query';
import { renderHook, waitFor } from '@testing-library/react-native';

import type { DiscoveryResult, DiscoverySource } from '@shared/apiClient/discovery';

import {
  DETAIL_HEALTH_BATCH,
  _resetDetailHealthForTest,
  fetchTallyingOutcome,
  flushDetailHealth,
  recordContentFetchOutcome,
} from '@features/detail/domain/detailHealth';
import { useAlbumTracks } from '@features/detail/hooks/useAlbumTracks';
import { useDetailEnrichments, useEnrichment } from '@features/detail/hooks/useDetailEnrichments';
import { useArtistContent } from '@features/detail/hooks/useArtistContent';
import { createTestQueryClient, createWrapper, signInTestUser } from '@testing/queryHarness';

const { __http } = require('@testing/doubles/fetch.js');

type AppStateChangeHandler = (state: string) => void;

jest.mock('react-native/Libraries/AppState/AppState', () => {
  const listeners: AppStateChangeHandler[] = [];
  return {
    default: {
      currentState: 'active',
      isAvailable: true,
      addEventListener: jest.fn((type: string, handler: AppStateChangeHandler) => {
        if (type === 'change') listeners.push(handler);
        return { remove: jest.fn() };
      }),
    },
    __listeners: listeners,
  };
});

const EVENTS = 'POST /v1/discovery/events';

const ALBUM_TRACKS_PATH = 'GET /v1/discovery/albums/spotify/album-1/tracks';

function appStateListeners(): AppStateChangeHandler[] {
  return (
    jest.requireMock('react-native/Libraries/AppState/AppState') as {
      __listeners: AppStateChangeHandler[];
    }
  ).__listeners;
}

function albumResult(): DiscoveryResult {
  return {
    kind: 'album',
    title: 'OK Computer',
    subtitle: 'Radiohead',
    image_url: null,
    confidence: 'high',
    sources: [],
    extras: {},
  };
}

function emptyTallyPayload(): Record<string, number> {
  return {
    enrichment_musicbrainz_ok: 0,
    enrichment_musicbrainz_failed: 0,
    enrichment_deezer_ok: 0,
    enrichment_deezer_failed: 0,
    enrichment_lastfm_ok: 0,
    enrichment_lastfm_failed: 0,
    content_album_tracks_ok: 0,
    content_album_tracks_failed: 0,
    content_artist_content_ok: 0,
    content_artist_content_failed: 0,
    content_related_tracks_ok: 0,
    content_related_tracks_failed: 0,
  };
}

function sentPayloads(): Record<string, unknown>[] {
  return (__http.requests as { path: string; method: string; body: string }[])
    .filter((request) => request.method === 'POST' && request.path === '/v1/discovery/events')
    .map((request) => {
      const event = JSON.parse(request.body) as {
        type: string;
        payload: Record<string, unknown>;
      };
      expect(event.type).toBe('detail_health');
      return Object.fromEntries(
        Object.entries(event.payload).filter(([key]) => key !== 'session_id'),
      );
    });
}

function albumTracksResponse(status: string): Record<string, unknown> {
  return { items: [], provider_name: 'spotify', status };
}

function emptyEnrichmentResponse(): Record<string, unknown> {
  return {
    has_content: false,
    mbid: '',
    genres: [],
    year: 0,
    rating: 0,
    rating_votes: 0,
    primary_type: '',
    secondary_types: [],
    external_ids: {},
    artwork_url: '',
  };
}

const SETTLE_TICKS = 5;
const BATCHES_SENT = 2;
const PARTIAL_BATCH = 3;

async function settle(): Promise<void> {
  for (let i = 0; i < SETTLE_TICKS; i++) await new Promise((resolve) => setImmediate(resolve));
}

beforeEach(async () => {
  _resetDetailHealthForTest();
  __http.reply(EVENTS, { status: 204 });
  await signInTestUser();
  jest.spyOn(console, 'warn').mockImplementation(() => undefined);
});

afterEach(() => {
  jest.restoreAllMocks();
});

describe('detail health metric', () => {
  it('tallies each provider of one detail render, served and failed alike', async () => {
    __http.reply('GET /v1/discovery/enrichment', {
      status: 200,
      json: emptyEnrichmentResponse(),
    });
    __http.fail('GET /v1/discovery/enrichment/deezer');

    const { result } = renderHook(() => useDetailEnrichments(albumResult()), {
      wrapper: createWrapper(createTestQueryClient()),
    });
    await waitFor(() => expect(result.current.errors.deezer).toBe(true));
    await waitFor(() => expect(__http.countFor('GET /v1/discovery/enrichment')).toBe(1));
    flushDetailHealth();

    await waitFor(() =>
      expect(sentPayloads()).toEqual([
        { ...emptyTallyPayload(), enrichment_musicbrainz_ok: 1, enrichment_deezer_failed: 1 },
      ]),
    );
  });

  it('tallies a served content fetch as a success', async () => {
    __http.reply(ALBUM_TRACKS_PATH, { status: 200, json: albumTracksResponse('ok') });

    const { result } = renderHook(
      () => useAlbumTracks({ provider: 'spotify', externalId: 'album-1' }),
      { wrapper: createWrapper(createTestQueryClient()) },
    );
    await waitFor(() => expect(result.current.isLoading).toBe(false));
    flushDetailHealth();

    await waitFor(() =>
      expect(sentPayloads()).toEqual([
        expect.objectContaining({ content_album_tracks_ok: 1, content_album_tracks_failed: 0 }),
      ]),
    );
  });

  it('tallies a degraded provider status as a failed content fetch', async () => {
    __http.reply(ALBUM_TRACKS_PATH, { status: 200, json: albumTracksResponse('timeout') });

    const { result } = renderHook(
      () => useAlbumTracks({ provider: 'spotify', externalId: 'album-1' }),
      { wrapper: createWrapper(createTestQueryClient()) },
    );
    await waitFor(() => expect(result.current.isError).toBe(true));
    flushDetailHealth();

    await waitFor(() =>
      expect(sentPayloads()).toEqual([
        expect.objectContaining({ content_album_tracks_failed: 1, content_album_tracks_ok: 0 }),
      ]),
    );
  });

  it('tallies neither outcome for a fetch the screen aborted', async () => {
    await expect(
      fetchTallyingOutcome('related_tracks', () => Promise.reject(__http.abortError())),
    ).rejects.toThrow();
    flushDetailHealth();

    await settle();
    expect(sentPayloads()).toEqual([]);
  });

  it('sends one aggregate event per batch, not one per fetch', async () => {
    for (let i = 0; i < DETAIL_HEALTH_BATCH * BATCHES_SENT + PARTIAL_BATCH; i++) {
      recordContentFetchOutcome('related_tracks', i % 5 !== 0);
    }

    await waitFor(() => expect(sentPayloads()).toHaveLength(BATCHES_SENT));
    expect(sentPayloads()[0]).toMatchObject({
      content_related_tracks_ok: 20,
      content_related_tracks_failed: 5,
    });
  });

  it('flushes the partial batch when the app goes to the background', async () => {
    recordContentFetchOutcome('artist_content', false);
    await settle();
    expect(sentPayloads()).toEqual([]);

    for (const listener of appStateListeners()) listener('background');

    await waitFor(() =>
      expect(sentPayloads()).toEqual([
        expect.objectContaining({ content_artist_content_failed: 1 }),
      ]),
    );
  });

  it('sends nothing when there is nothing to report, and swallows a failed send', async () => {
    flushDetailHealth();
    await settle();
    expect(sentPayloads()).toEqual([]);

    __http.reset();
    __http.fail(EVENTS);
    recordContentFetchOutcome('album_tracks', true);
    expect(() => flushDetailHealth()).not.toThrow();
    await new Promise((resolve) => setImmediate(resolve));
    flushDetailHealth();

    await waitFor(() => expect(sentPayloads()).toHaveLength(1));
    await waitFor(() =>
      expect(console.warn).toHaveBeenCalledWith('[detail] health flush failed', {
        error: expect.any(Error),
      }),
    );
    await settle();
    expect(sentPayloads()).toHaveLength(1);
  });
});

describe('an aborted fetch', () => {
  let client: QueryClient;

  let wrapper: ReturnType<typeof createWrapper>;

  beforeEach(async () => {
    _resetDetailHealthForTest();
    __http.reply(EVENTS, { status: 204 });
    await signInTestUser();
    client = createTestQueryClient();
    wrapper = createWrapper(client);
  });

  afterEach(() => client.clear());

  describe('a fetch aborted by leaving the screen', () => {
    it('is tallied as neither success nor failure', async () => {
      __http.hang('GET /v1/discovery/enrichment');
      __http.hang('GET /v1/discovery/artists/spotify/a-1/content');
      const { unmount } = renderHook(
        () => {
          useEnrichment({ kind: 'album', title: 'OK Computer' });
          useArtistContent({
            sources: [{ provider: 'spotify', external_id: 'a-1' } as DiscoverySource],
          });
        },
        { wrapper },
      );
      await waitFor(() => expect(__http.requests).toHaveLength(2));

      unmount();
      await waitFor(() =>
        expect((__http.requests as { signal: AbortSignal }[]).every((r) => r.signal.aborted)).toBe(
          true,
        ),
      );
      flushDetailHealth();

      await settle();
      expect(sentPayloads()).toEqual([]);
    });
  });
});

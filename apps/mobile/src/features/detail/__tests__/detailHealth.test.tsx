// Regression for issue #1665: enrichment and detail content fetches fall back to an empty
// section silently, so their outcomes must be tallied into an aggregate `detail_health` event
// from which a per-provider success rate can be computed — one event per batch, never one per
// fetch.

import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { renderHook, waitFor } from '@testing-library/react-native';
import type { ReactNode } from 'react';

import type { DiscoveryResult, DiscoverySource } from '@shared/api-client/discovery';
import { supabase } from '@shared/auth/supabaseClient';
import { recordEvent } from '@shared/telemetry/recordEvent';

import {
  DETAIL_HEALTH_BATCH,
  _resetDetailHealthForTest,
  fetchTallyingOutcome,
  flushDetailHealth,
  recordContentFetchOutcome,
} from '../detailHealth';
import { useAlbumTracks } from '../hooks/useAlbumTracks';
import { useDetailEnrichments } from '../hooks/useDetailEnrichments';
import { useArtistContent } from '../hooks/useArtistContent';
import { useEnrichment } from '../hooks/useEnrichment';

const { __http } = require('../../../../jest/doubles/fetch.js');

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

jest.mock('@shared/telemetry/recordEvent', () => ({ recordEvent: jest.fn() }));

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn() } },
}));

const recordEventMock = recordEvent as jest.MockedFunction<typeof recordEvent>;

const ALBUM_TRACKS_PATH = 'GET /v1/discovery/albums/spotify/album-1/tracks';

function appStateListeners(): AppStateChangeHandler[] {
  return (
    jest.requireMock('react-native/Libraries/AppState/AppState') as {
      __listeners: AppStateChangeHandler[];
    }
  ).__listeners;
}

function createWrapper(queryClient: QueryClient) {
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
  };
}

function freshClient() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } });
}

function artistResult(): DiscoveryResult {
  return {
    kind: 'artist',
    title: 'Radiohead',
    subtitle: null,
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
  return recordEventMock.mock.calls.map(([event]) => {
    expect(event.type).toBe('detail_health');
    return event.payload ?? {};
  });
}

function albumTracksResponse(status: string): Record<string, unknown> {
  return { items: [], provider_name: 'spotify', status };
}

// A MusicBrainz lookup that found nothing still answers with the full DTO.
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

beforeEach(() => {
  _resetDetailHealthForTest();
  recordEventMock.mockReset().mockResolvedValue(undefined);
  (supabase.auth.getSession as jest.Mock).mockReset().mockResolvedValue({
    data: { session: { access_token: 'tok' } },
    error: null,
  });
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
    __http.fail('GET /v1/discovery/enrichment/lastfm');

    const { result } = renderHook(() => useDetailEnrichments(artistResult()), {
      wrapper: createWrapper(freshClient()),
    });
    await waitFor(() => expect(result.current.errors.lastfm).toBe(true));
    flushDetailHealth();

    expect(sentPayloads()).toEqual([
      { ...emptyTallyPayload(), enrichment_musicbrainz_ok: 1, enrichment_lastfm_failed: 1 },
    ]);
  });

  it('tallies a served content fetch as a success', async () => {
    __http.reply(ALBUM_TRACKS_PATH, { status: 200, json: albumTracksResponse('ok') });

    const { result } = renderHook(
      () => useAlbumTracks({ provider: 'spotify', externalId: 'album-1' }),
      { wrapper: createWrapper(freshClient()) },
    );
    await waitFor(() => expect(result.current.isLoading).toBe(false));
    flushDetailHealth();

    expect(sentPayloads()).toEqual([
      expect.objectContaining({ content_album_tracks_ok: 1, content_album_tracks_failed: 0 }),
    ]);
  });

  it('tallies a degraded provider status as a failed content fetch', async () => {
    __http.reply(ALBUM_TRACKS_PATH, { status: 200, json: albumTracksResponse('timeout') });

    const { result } = renderHook(
      () => useAlbumTracks({ provider: 'spotify', externalId: 'album-1' }),
      { wrapper: createWrapper(freshClient()) },
    );
    await waitFor(() => expect(result.current.isError).toBe(true));
    flushDetailHealth();

    expect(sentPayloads()).toEqual([
      expect.objectContaining({ content_album_tracks_failed: 1, content_album_tracks_ok: 0 }),
    ]);
  });

  it('tallies neither outcome for a fetch the screen aborted', async () => {
    await expect(
      fetchTallyingOutcome('related_tracks', () => Promise.reject(__http.abortError())),
    ).rejects.toThrow();
    flushDetailHealth();

    expect(recordEventMock).not.toHaveBeenCalled();
  });

  it('sends one aggregate event per batch, not one per fetch', () => {
    for (let i = 0; i < DETAIL_HEALTH_BATCH * 2 + 3; i++) {
      recordContentFetchOutcome('related_tracks', i % 5 !== 0);
    }

    const payloads = sentPayloads();
    expect(payloads).toHaveLength(2);
    expect(payloads[0]).toMatchObject({
      content_related_tracks_ok: 20,
      content_related_tracks_failed: 5,
    });
  });

  it('flushes the partial batch when the app goes to the background', () => {
    recordContentFetchOutcome('artist_content', false);
    expect(recordEventMock).not.toHaveBeenCalled();

    for (const listener of appStateListeners()) listener('background');

    expect(sentPayloads()).toEqual([
      expect.objectContaining({ content_artist_content_failed: 1 }),
    ]);
  });

  it('sends nothing when there is nothing to report, and swallows a failed send', async () => {
    flushDetailHealth();
    expect(recordEventMock).not.toHaveBeenCalled();

    recordEventMock.mockRejectedValueOnce(new Error('offline'));
    recordContentFetchOutcome('album_tracks', true);
    expect(() => flushDetailHealth()).not.toThrow();
    await Promise.resolve();
    flushDetailHealth();

    expect(recordEventMock).toHaveBeenCalledTimes(1);
  });
});

describe('an aborted fetch', () => {
  let client: QueryClient;

  function wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
  }

  beforeEach(() => {
    (supabase.auth.getSession as jest.Mock).mockResolvedValue({
      data: { session: { access_token: 'tok' } },
      error: null,
    });
    _resetDetailHealthForTest();
    (recordEvent as jest.Mock).mockReset().mockResolvedValue(undefined);
    client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
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

      expect(recordEvent).not.toHaveBeenCalled();
    });
  });
});

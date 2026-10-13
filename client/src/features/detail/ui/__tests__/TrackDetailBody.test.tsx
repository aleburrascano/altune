import React from 'react';
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react-native';

import type { DiscoveryResult } from '@shared/apiClient/discovery';
import { asTrackId } from '@shared/apiClient/ids';
import {
  linkTrackIdentity,
  patchTrackStatus,
  trackIdentityKey,
  useTrackStatusStore,
} from '@shared/acquisition/trackStatusStore';

import { optimisticTrackId, toCreateTrackRequest } from '@features/detail/domain/saveTrack';
import type { LateralNavHandle } from '@features/detail/hooks/useTrackDetailActions';
import { RelatedTracksSection } from '@features/detail/ui/RelatedTracksSection';
import { TrackDetailBody } from '@features/detail/ui/TrackDetailBody';
import { readDetailHandoff } from '@shared/navigation/detailHandoff';
import {
  createTestQueryClient,
  createPlaybackWrapper,
  signInTestUser,
} from '@testing/queryHarness';

const { __http } = require('@testing/doubles/fetch.js');

jest.setTimeout(20000);

function resultCarriedBy(handoffId: string | string[] | undefined): string | null {
  return readDetailHandoff(handoffId)?.result.title ?? null;
}

jest.mock('expo-router', () => ({ useRouter: () => ({ push: jest.fn() }) }));

interface RecordedRequest {
  path: string;
}

const TITLE = 'Midnight City';
const ARTIST = 'M83';

function trackResult(): DiscoveryResult {
  return {
    kind: 'track',
    title: TITLE,
    subtitle: ARTIST,
    image_url: null,
    confidence: 'high',
    sources: [],
    extras: {},
  };
}

const lateralNav: LateralNavHandle = {
  navigateTo: async () => {},
  state: 'idle',
  error: null,
};

function renderDetail(): void {
  render(
    <TrackDetailBody
      chrome={{ title: TITLE, artworkUrl: null, onBack: () => {} }}
      result={trackResult()}
      lateralNav={lateralNav}
      detailRoute="/discover/detail"
    />,
    { wrapper: createPlaybackWrapper(createTestQueryClient({ mutations: true })) },
  );
}

async function pressSave(): Promise<void> {
  await act(async () => {
    fireEvent.press(screen.getByTestId('detail-save'));
  });
}

function banner(): ReturnType<typeof within> {
  return within(screen.getByTestId('detail-save-error'));
}

function saveIsInteractive(): boolean {
  return screen.getByTestId('detail-save').props.accessibilityState.disabled === false;
}

let warnSpy: jest.SpyInstance;

beforeEach(async () => {
  await signInTestUser();
  __http.replyAll({ status: 204 });
  warnSpy = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
  useTrackStatusStore.getState().reset();
});

afterEach(() => {
  warnSpy.mockRestore();
  useTrackStatusStore.getState().reset();
});

describe('TrackDetailBody save failure', () => {
  it('offers a retry and says so when the save failed for a transient reason', async () => {
    __http.reply('POST /v1/tracks', { status: 503 });
    renderDetail();

    await pressSave();

    await waitFor(() => expect(screen.getByTestId('detail-save-error')).toBeTruthy());
    expect(banner().getByText(/Tap Retry\./)).toBeTruthy();
    expect(banner().getByText(/returned 503/)).toBeTruthy();
    expect(saveIsInteractive()).toBe(true);
    expect(screen.getByLabelText(`Retry saving ${TITLE}`)).toBeTruthy();
  });

  it('offers no retry and names the refusal when the save failed permanently', async () => {
    __http.reply('POST /v1/tracks', { status: 400, json: { code: 'duplicate_track' } });
    renderDetail();

    await pressSave();

    await waitFor(() => expect(screen.getByTestId('detail-save-error')).toBeTruthy());
    expect(banner().getByText(/Retrying won't help\./)).toBeTruthy();
    expect(banner().getByText(/returned 400/)).toBeTruthy();
    expect(saveIsInteractive()).toBe(false);
    expect(screen.queryByLabelText(`Retry saving ${TITLE}`)).toBeNull();
    expect(screen.getByLabelText(`Couldn't save ${TITLE}`)).toBeTruthy();
  });

  it('does not re-POST when the control for a permanently refused save is pressed again', async () => {
    __http.reply('POST /v1/tracks', { status: 400, json: { code: 'duplicate_track' } });
    renderDetail();

    await pressSave();
    await waitFor(() => expect(screen.getByTestId('detail-save-error')).toBeTruthy());
    await pressSave();

    expect(__http.countFor('POST /v1/tracks')).toBe(1);
  });
});

const OWNED_TITLE = 'Rollacoasta';
const OWNED_TRACK_ID = asTrackId('owned-track-1');

function renderOwnedFailedDetail(): void {
  const result: DiscoveryResult = {
    ...trackResult(),
    title: OWNED_TITLE,
    subtitle: 'Grip',
    extras: { owned_track_id: OWNED_TRACK_ID, owned_acquisition_status: 'failed' },
  };
  render(
    <TrackDetailBody
      chrome={{ title: OWNED_TITLE, artworkUrl: null, onBack: () => {} }}
      result={result}
      lateralNav={lateralNav}
      detailRoute="/discover/detail"
    />,
    { wrapper: createPlaybackWrapper(createTestQueryClient({ mutations: true })) },
  );
}

describe('TrackDetailBody retry on an owned, failed track (#2852)', () => {
  it('hits the retry endpoint with the owned trackId, never a create', async () => {
    __http.reply(`POST /v1/tracks/${OWNED_TRACK_ID}/retry`, { status: 202 });
    renderOwnedFailedDetail();

    await pressSave();

    await waitFor(() => expect(__http.countFor(`POST /v1/tracks/${OWNED_TRACK_ID}/retry`)).toBe(1));
    expect(__http.countFor('POST /v1/tracks')).toBe(0);
  });

  it('shows the pill as downloading once the retry is dispatched', async () => {
    __http.hang(`POST /v1/tracks/${OWNED_TRACK_ID}/retry`);
    renderOwnedFailedDetail();

    await pressSave();

    await waitFor(() => expect(screen.getByLabelText(`${OWNED_TITLE} downloading`)).toBeTruthy());
  });
});

function renderDetailOf(result: DiscoveryResult, title: string = result.title): void {
  render(
    <TrackDetailBody
      chrome={{ title, artworkUrl: null, onBack: () => {} }}
      result={result}
      lateralNav={lateralNav}
      detailRoute="/discover/detail"
    />,
    { wrapper: createPlaybackWrapper(createTestQueryClient({ mutations: true })) },
  );
}

function savedTrackResponse(acquisitionStatus: 'pending' | 'ready') {
  return {
    id: 'srv-midnight-city',
    title: TITLE,
    artist: ARTIST,
    album: null,
    duration_seconds: null,
    added_at: '2024-01-01T00:00:00Z',
    acquisition_status: acquisitionStatus,
    artwork_url: null,
    failure_reason: null,
    year: null,
    genre: null,
    track_number: null,
    album_artist: null,
    isrc: null,
    audio_ref: null,
  };
}

describe('TrackDetailBody save tap guard', () => {
  it('sends nothing when save is pressed on a track with no known artist', async () => {
    __http.reply('POST /v1/tracks', { status: 503 });
    renderDetailOf({ ...trackResult(), subtitle: null });

    expect(saveIsInteractive()).toBe(false);
    await pressSave();

    expect(__http.countFor('POST /v1/tracks')).toBe(0);
  });

  it('sends nothing when save is pressed on a track already in the library', async () => {
    __http.reply('POST /v1/tracks', { status: 503 });
    renderDetailOf({
      ...trackResult(),
      extras: { owned_track_id: asTrackId('owned-ready-1'), owned_acquisition_status: 'ready' },
    });

    expect(screen.getByLabelText(`${TITLE} in library`)).toBeTruthy();
    expect(saveIsInteractive()).toBe(false);
    await pressSave();

    expect(__http.countFor('POST /v1/tracks')).toBe(0);
    expect(__http.countFor('POST /v1/tracks/owned-ready-1/retry')).toBe(0);
  });

  it('sends one save when the pill is tapped again while the first is still saving', async () => {
    __http.hang('POST /v1/tracks');
    renderDetail();

    await pressSave();
    await waitFor(() => expect(screen.getByLabelText(`${TITLE} downloading`)).toBeTruthy());
    expect(saveIsInteractive()).toBe(false);
    await pressSave();

    expect(__http.countFor('POST /v1/tracks')).toBe(1);
  });
});

describe('TrackDetailBody retry after a failed save', () => {
  it('saves again on Retry and clears the failure once the retry is accepted', async () => {
    __http.replyOnce('POST /v1/tracks', { status: 503 });
    __http.reply('POST /v1/tracks', { status: 200, json: savedTrackResponse('pending') });
    __http.reply('POST /v1/discovery/events', { status: 202 });
    renderDetail();

    await pressSave();
    await waitFor(() => expect(screen.getByLabelText(`Retry saving ${TITLE}`)).toBeTruthy());
    await pressSave();

    await waitFor(() => expect(screen.getByLabelText(`${TITLE} downloading`)).toBeTruthy());
    expect(__http.countFor('POST /v1/tracks')).toBe(2);
    expect(screen.queryByTestId('detail-save-error')).toBeNull();
  });

  it('keeps offering Retry when the retry fails again', async () => {
    __http.reply('POST /v1/tracks', { status: 503 });
    renderDetail();

    await pressSave();
    await waitFor(() => expect(screen.getByLabelText(`Retry saving ${TITLE}`)).toBeTruthy());
    await pressSave();

    await waitFor(() => expect(__http.countFor('POST /v1/tracks')).toBe(2));
    await waitFor(() => expect(screen.getByLabelText(`Retry saving ${TITLE}`)).toBeTruthy());
    expect(saveIsInteractive()).toBe(true);
    expect(banner().getByText(/Tap Retry\./)).toBeTruthy();
  });
});

describe('TrackDetailBody pins its router calls', () => {
  it('pushes the featuring route naming the tapped featured artist', async () => {
    const push = jest.fn();
    jest.spyOn(require('expo-router'), 'useRouter').mockReturnValue({ push });

    render(
      <TrackDetailBody
        chrome={{ title: TITLE, artworkUrl: null, onBack: () => {} }}
        result={{
          ...trackResult(),
          extras: { featured_artists: [{ name: 'Guest Artist', mbid: 'mb-guest' }] },
        }}
        lateralNav={lateralNav}
        detailRoute="/discover/detail"
      />,
      { wrapper: createPlaybackWrapper(createTestQueryClient()) },
    );

    fireEvent.press(screen.getByLabelText('Tracks featuring Guest Artist'));

    expect(push).toHaveBeenCalledWith({
      pathname: '/discover/featuring',
      params: { name: 'Guest Artist', mbid: 'mb-guest' },
    });
  });

  it('pushes the exact related track that was tapped', async () => {
    const push = jest.fn();
    jest.spyOn(require('expo-router'), 'useRouter').mockReturnValue({ push });

    __http.reply('GET /v1/discovery/tracks/soundcloud/sc-1/related', {
      status: 200,
      json: {
        items: [
          {
            kind: 'track',
            title: 'Related One',
            subtitle: 'Someone Else',
            image_url: null,
            confidence: 'high',
            sources: [],
            extras: {},
          },
        ],
        provider_name: 'soundcloud',
        status: 'ok',
      },
    });

    const result: DiscoveryResult = {
      ...trackResult(),
      sources: [{ provider: 'soundcloud', external_id: 'sc-1', url: 'https://sc/sc-1' }],
    };

    render(<RelatedTracksSection result={result} detailRoute="/discover/detail" />, {
      wrapper: createPlaybackWrapper(createTestQueryClient()),
    });

    fireEvent.press(await screen.findByTestId('detail-related-0'));

    expect(push).toHaveBeenCalledTimes(1);
    const [href] = push.mock.calls[0]!;
    expect(href.pathname).toBe('/discover/detail');
    const { readDetailHandoff } = jest.requireActual('@shared/navigation/detailHandoff');
    expect(readDetailHandoff(href.params.handoff)?.result.title).toBe('Related One');
    expect(resultCarriedBy(href.params.handoff)).toBe('Related One');
  });
});

describe('TrackDetailBody add to playlist during a pending save', () => {
  it('resolves to the server-issued id, never the optimistic placeholder', async () => {
    const placeholderId = optimisticTrackId(toCreateTrackRequest(trackResult()));
    patchTrackStatus(
      placeholderId,
      { acquisitionStatus: 'pending', failureMessage: null },
      'optimistic',
    );
    linkTrackIdentity(trackIdentityKey(TITLE, ARTIST), placeholderId);
    __http.reply('POST /v1/tracks', { status: 201, json: savedTrackResponse('pending') });
    __http.reply('GET /v1/playlists', {
      status: 200,
      json: {
        items: [
          {
            id: 'pl-1',
            name: 'Night Drive',
            track_count: 0,
            preview_artwork_urls: [],
            created_at: '2024-01-01T00:00:00Z',
            updated_at: '2024-01-01T00:00:00Z',
          },
        ],
        total: 1,
      },
    });
    __http.reply('POST /v1/playlists/pl-1/tracks/batch', {
      status: 200,
      json: { added: 1, skipped: 0 },
    });
    renderDetail();

    fireEvent.press(screen.getByTestId('detail-add-to-playlist'));
    fireEvent.press(await screen.findByTestId('add-to-playlist-pl-1'));

    await waitFor(() => expect(__http.countFor('POST /v1/playlists/pl-1/tracks/batch')).toBe(1));
    const batch = __http.requests.find(
      (request: RecordedRequest) => request.path === '/v1/playlists/pl-1/tracks/batch',
    );
    expect(JSON.parse(batch.body)).toEqual({ track_ids: ['srv-midnight-city'] });
  });
});

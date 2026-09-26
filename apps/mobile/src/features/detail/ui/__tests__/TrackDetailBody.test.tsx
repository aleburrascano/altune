// #1661: every save failure used to render one hardcoded banner and an
// always-tappable Retry, so a permanent refusal (a 400 the server will give
// again) was indistinguishable from a transient 503 — and the retry it offered
// could never work.

import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react-native';

import type { DiscoveryResult } from '@shared/api-client/discovery';
import { supabase } from '@shared/auth/supabaseClient';
import { asTrackId } from '@shared/api-client/ids';
import { useTrackStatusStore } from '@shared/acquisition/trackStatusStore';

import type { LateralNavHandle } from '../../hooks/useTrackDetailActions';
import { TrackDetailBody } from '../TrackDetailBody';

const { __http } = require('../../../../../jest/doubles/fetch.js');

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn() } },
}));
jest.mock('expo-router', () => ({ useRouter: () => ({ push: jest.fn() }) }));
jest.mock('@shared/playback/usePlayback', () => ({
  usePlayback: () => ({ status: 'idle', play: jest.fn(), pause: jest.fn() }),
}));
jest.mock('@shared/playlists', () => ({ AddToPlaylistSheet: () => null }));
jest.mock('@shared/telemetry/outbox', () => ({ enqueueCritical: jest.fn() }));
jest.mock('../RelatedTracksSection', () => ({ RelatedTracksSection: () => null }));
jest.mock('../DetailScaffold', () => {
  const { View } = jest.requireActual('react-native');
  return {
    DetailScaffold: ({ actions, children }: { actions: unknown; children: unknown }) => (
      <View>
        {actions}
        {children}
      </View>
    ),
  };
});

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
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  render(
    <QueryClientProvider client={queryClient}>
      <TrackDetailBody
        chrome={{ title: TITLE, artworkUrl: null, onBack: () => {} }}
        result={trackResult()}
        lateralNav={lateralNav}
        detailRoute="/discover/detail"
      />
    </QueryClientProvider>,
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

beforeEach(() => {
  (supabase.auth.getSession as jest.Mock).mockResolvedValue({
    data: { session: { access_token: 'tok' } },
    error: null,
  });
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
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  const result: DiscoveryResult = {
    ...trackResult(),
    title: OWNED_TITLE,
    subtitle: 'Grip',
    extras: { owned_track_id: OWNED_TRACK_ID, owned_acquisition_status: 'failed' },
  };
  render(
    <QueryClientProvider client={queryClient}>
      <TrackDetailBody
        chrome={{ title: OWNED_TITLE, artworkUrl: null, onBack: () => {} }}
        result={result}
        lateralNav={lateralNav}
        detailRoute="/discover/detail"
      />
    </QueryClientProvider>,
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
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  render(
    <QueryClientProvider client={queryClient}>
      <TrackDetailBody
        chrome={{ title, artworkUrl: null, onBack: () => {} }}
        result={result}
        lateralNav={lateralNav}
        detailRoute="/discover/detail"
      />
    </QueryClientProvider>,
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

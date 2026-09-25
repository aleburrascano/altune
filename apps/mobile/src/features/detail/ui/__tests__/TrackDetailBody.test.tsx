// #1661: every save failure used to render one hardcoded banner and an
// always-tappable Retry, so a permanent refusal (a 400 the server will give
// again) was indistinguishable from a transient 503 — and the retry it offered
// could never work.

import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react-native';

import type { DiscoveryResult } from '@shared/api-client/discovery';
import { supabase } from '@shared/auth/supabaseClient';
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

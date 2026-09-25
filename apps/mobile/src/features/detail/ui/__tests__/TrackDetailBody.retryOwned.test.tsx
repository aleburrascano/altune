// #2852: the detail Retry pill on a track already owned and `failed` used to
// re-POST a create, which the server dedups onto the existing failed row and
// schedules nothing. It must hit the retry acquisition endpoint instead.

import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react-native';

import type { DiscoveryResult } from '@shared/api-client/discovery';
import { asTrackId } from '@shared/api-client/ids';
import { useTrackStatusStore } from '@shared/acquisition/trackStatusStore';

import type { LateralNavHandle } from '../../hooks/useTrackDetailActions';
import { TrackDetailBody } from '../TrackDetailBody';

const mockCreateTrack = jest.fn();
const mockRetryAcquisition = jest.fn<Promise<void>, [unknown]>();
jest.mock('@shared/api-client/tracks', () => ({
  createTrack: (...args: unknown[]) => mockCreateTrack(...args),
  retryAcquisition: (trackId: unknown) => mockRetryAcquisition(trackId),
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

const TITLE = 'Rollacoasta';
const ARTIST = 'Grip';
const TRACK_ID = asTrackId('owned-track-1');

function ownedFailedResult(): DiscoveryResult {
  return {
    kind: 'track',
    title: TITLE,
    subtitle: ARTIST,
    image_url: null,
    confidence: 'high',
    sources: [],
    extras: { owned_track_id: TRACK_ID, owned_acquisition_status: 'failed' },
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
        result={ownedFailedResult()}
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

beforeEach(() => {
  mockCreateTrack.mockReset();
  mockRetryAcquisition.mockReset();
  useTrackStatusStore.getState().reset();
});

describe('TrackDetailBody retry on an owned, failed track', () => {
  it('hits the retry endpoint with the owned trackId, never a create', async () => {
    mockRetryAcquisition.mockResolvedValue(undefined);
    renderDetail();

    await pressSave();

    expect(mockRetryAcquisition).toHaveBeenCalledWith(TRACK_ID);
    expect(mockCreateTrack).not.toHaveBeenCalled();
  });

  it('shows the pill as saving once the retry is dispatched', async () => {
    mockRetryAcquisition.mockReturnValue(new Promise(() => undefined));
    renderDetail();

    await pressSave();

    await waitFor(() =>
      expect(screen.getByLabelText(`${TITLE} downloading`)).toBeTruthy(),
    );
  });
});

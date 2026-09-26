// #1702: a list shares one retry/re-acquire mutation, whose pending state named only the
// last tapped row. Tapping Retry on a second row put the first row's button back while its
// request was still in flight, so the same track could be retried twice concurrently.

import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, fireEvent, render, renderHook, screen, waitFor } from '@testing-library/react-native';

import { asTrackId, type TrackId } from '@shared/api-client/ids';
import type { TrackResponse } from '@shared/api-client/types';

import { useReacquireTrack } from '../hooks/useReacquireTrack';
import { useRetryAcquisition } from '../hooks/useRetryAcquisition';
import { TracksList } from '../ui/TracksList';

const mockRetryAcquisition = jest.fn<Promise<void>, [TrackId]>();
const mockReacquireTrack = jest.fn<Promise<void>, [TrackId]>();
jest.mock('@shared/api-client/tracks', () => ({
  retryAcquisition: (id: TrackId) => mockRetryAcquisition(id),
  reacquireTrack: (id: TrackId) => mockReacquireTrack(id),
}));
jest.mock('@shared/telemetry/outbox', () => ({ enqueueCritical: jest.fn() }));

function failedTrack(id: string): TrackResponse {
  return {
    id: asTrackId(id),
    title: `Track ${id}`,
    artist: 'An Artist',
    album: null,
    duration_seconds: 180,
    added_at: '2026-01-01T00:00:00Z',
    acquisition_status: 'failed',
    artwork_url: null,
    failure_reason: 'no_source',
    failure_message: 'No source found',
    year: null,
    genre: null,
    track_number: null,
    album_artist: null,
    isrc: null,
    audio_ref: null,
  } as TrackResponse;
}

function setup() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  function Wrapper({ children }: { children: React.ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
  }
  return { wrapper: Wrapper };
}

function FailedTracksList({ tracks }: { tracks: TrackResponse[] }) {
  const retry = useRetryAcquisition();
  return (
    <TracksList
      tracks={tracks}
      emptyLabel=""
      refresh={{ refreshing: false, onRefresh: jest.fn() }}
      onPlay={jest.fn()}
      onPress={jest.fn()}
      onMore={jest.fn()}
      onRetry={(track) => retry.mutate(track.id)}
      isRetrying={retry.isInFlight}
      isPlaying={() => false}
    />
  );
}

const retryButton = (id: string) => screen.queryByTestId(`library-row-retry-${id}`);
const retriedIds = (): TrackId[] => mockRetryAcquisition.mock.calls.map(([id]) => id);

function tapRetry(id: string): void {
  const button = retryButton(id);
  if (button === null) return;
  fireEvent.press(button);
}

// The mutation cache notifies its subscribers on a timer, so the rows that show a
// spinner instead of a button are one turn of the event loop behind the tap.
const rowsCatchUp = async (): Promise<void> => {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 0));
  });
};

beforeEach(() => {
  mockRetryAcquisition.mockReset();
  mockReacquireTrack.mockReset();
  mockRetryAcquisition.mockReturnValue(new Promise(() => undefined));
  mockReacquireTrack.mockReturnValue(new Promise(() => undefined));
});

describe('retry acquisition — a row in flight does not speak for the others', () => {
  it('sends no second retry for a track whose first retry is still in flight', async () => {
    const { wrapper } = setup();
    render(<FailedTracksList tracks={[failedTrack('a'), failedTrack('b')]} />, { wrapper });

    tapRetry('a');
    tapRetry('b');
    await rowsCatchUp();
    tapRetry('a');
    await rowsCatchUp();

    expect(retriedIds()).toEqual(['a', 'b']);
  });

  it('keeps both tapped rows showing their own retry in progress', async () => {
    const { wrapper } = setup();
    render(<FailedTracksList tracks={[failedTrack('a'), failedTrack('b')]} />, { wrapper });

    tapRetry('a');
    tapRetry('b');

    await waitFor(() => {
      expect(screen.getByTestId('library-row-retrying-a')).toBeTruthy();
      expect(screen.getByTestId('library-row-retrying-b')).toBeTruthy();
    });
  });

  it('offers retry again on the row whose request landed, and only on that row', async () => {
    const { wrapper } = setup();
    let firstRetryLands!: () => void;
    mockRetryAcquisition.mockImplementationOnce(
      () => new Promise((resolve) => (firstRetryLands = () => resolve())),
    );
    render(<FailedTracksList tracks={[failedTrack('a'), failedTrack('b')]} />, { wrapper });

    tapRetry('a');
    tapRetry('b');
    await rowsCatchUp();
    firstRetryLands();

    await waitFor(() => expect(retryButton('a')).not.toBeNull());
    expect(retryButton('b')).toBeNull();
  });
});

describe('track mutations — one run per track id, whatever the taps do', () => {
  it('starts one retry per id when several taps land before any row re-renders', async () => {
    const { wrapper } = setup();
    const { result } = renderHook(() => useRetryAcquisition(), { wrapper });

    act(() => {
      result.current.mutate(asTrackId('a'));
      result.current.mutate(asTrackId('a'));
      result.current.mutate(asTrackId('b'));
    });
    await rowsCatchUp();

    expect(retriedIds()).toEqual(['a', 'b']);
  });

  it('reports every re-acquiring track as in flight, not just the last one started', async () => {
    const { wrapper } = setup();
    const { result } = renderHook(() => useReacquireTrack(), { wrapper });

    act(() => {
      result.current.mutate(asTrackId('a'));
      result.current.mutate(asTrackId('b'));
    });

    await waitFor(() => expect(result.current.isInFlight(asTrackId('a'))).toBe(true));
    expect(result.current.isInFlight(asTrackId('b'))).toBe(true);
    expect(result.current.isInFlight(asTrackId('c'))).toBe(false);
  });
});

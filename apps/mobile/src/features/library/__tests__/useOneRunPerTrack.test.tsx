import { act, fireEvent, render, renderHook, screen, waitFor } from '@testing-library/react-native';

import { asTrackId, type TrackId } from '@shared/api-client/ids';
import type { TrackResponse } from '@shared/api-client/trackResponse';
import { trackResponseFactory } from '@testing/factories';

import { useReacquireTrack } from '../hooks/useReacquireTrack';
import { useRetryAcquisition } from '../hooks/useRetryAcquisition';
import { TracksList } from '../ui/TracksList';
import { setup } from './trackMutationFixtures';

const mockRetryAcquisition = jest.fn<Promise<void>, [TrackId]>();
const mockReacquireTrack = jest.fn<Promise<void>, [TrackId]>();
const ports = {
  library: { reacquireTrack: mockReacquireTrack },
  acquisition: { retryAcquisition: mockRetryAcquisition },
};
jest.mock('@shared/telemetry/outbox', () => ({ enqueueCritical: jest.fn() }));

function failedTrack(id: string): TrackResponse {
  return trackResponseFactory.build({
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
  });
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
    const { wrapper } = setup(ports);
    render(<FailedTracksList tracks={[failedTrack('a'), failedTrack('b')]} />, { wrapper });

    tapRetry('a');
    tapRetry('b');
    await rowsCatchUp();
    tapRetry('a');
    await rowsCatchUp();

    expect(retriedIds()).toEqual(['a', 'b']);
  });

  it('keeps both tapped rows showing their own retry in progress', async () => {
    const { wrapper } = setup(ports);
    render(<FailedTracksList tracks={[failedTrack('a'), failedTrack('b')]} />, { wrapper });

    tapRetry('a');
    tapRetry('b');

    await waitFor(() => {
      expect(screen.getByTestId('library-row-retrying-a')).toBeTruthy();
      expect(screen.getByTestId('library-row-retrying-b')).toBeTruthy();
    });
  });

  it('offers retry again on the row whose request landed, and only on that row', async () => {
    const { wrapper } = setup(ports);
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
    const { wrapper } = setup(ports);
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
    const { wrapper } = setup(ports);
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

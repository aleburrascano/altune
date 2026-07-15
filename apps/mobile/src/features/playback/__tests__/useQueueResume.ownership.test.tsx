/**
 * Resume must lose to the user. The restore awaits a full-library rehydrate; a
 * tap during that window starts real playback, and restoring on top of it would
 * stop the music and swap in the saved queue, paused. It must also never persist
 * its one-track placeholder over the real saved queue.
 */

import { renderHook, waitFor } from '@testing-library/react-native';

const mockGetQueueState = jest.fn();
const mockSaveQueueState = jest.fn().mockResolvedValue(undefined);
const mockGetTracks = jest.fn();
const mockLoadNativeQueue = jest.fn().mockResolvedValue(undefined);

jest.mock('@shared/api-client/playback', () => ({
  getQueueState: (...a: unknown[]) => mockGetQueueState(...a),
  saveQueueState: (...a: unknown[]) => mockSaveQueueState(...a),
}));
jest.mock('@shared/api-client/tracks', () => ({
  getTracks: (...a: unknown[]) => mockGetTracks(...a),
}));
jest.mock('../loadNativeTrack', () => ({
  loadNativeQueue: (...a: unknown[]) => mockLoadNativeQueue(...a),
}));
jest.mock('react-native-track-player', () => ({
  __esModule: true,
  default: { getProgress: jest.fn().mockResolvedValue({ position: 0, duration: 0 }) },
}));

import { useQueueStore } from '@shared/playback/queueStore';
import { useQueueResume } from '../hooks/useQueueResume';

function savedTrack(id: string) {
  return {
    id,
    title: id,
    artist: `${id}-artist`,
    artwork_url: null,
    acquisition_status: 'ready',
    duration_seconds: 100,
  };
}

function deferred<T>() {
  let resolve!: (v: T) => void;
  const promise = new Promise<T>((r) => {
    resolve = r;
  });
  return { promise, resolve };
}

beforeEach(() => {
  jest.clearAllMocks();
  useQueueStore.setState({
    tracks: [],
    playOrder: [],
    currentIndex: -1,
    source: null,
    shuffled: false,
    repeatMode: 'off',
    resumePositionMs: 0,
    generation: 0,
  });
});

describe('resume vs. user-initiated playback', () => {
  it('abandons the restore when the user starts playing during the rehydrate', async () => {
    mockGetQueueState.mockResolvedValue({
      track_ids: ['saved-1', 'saved-2'],
      natural_order: ['saved-1', 'saved-2'],
      current_index: 0,
      position_ms: 90_000,
      shuffled: false,
      repeat_mode: 'off',
      source_id: 'library',
      current_track: null,
    });

    // Hold the slow full-library rehydrate open, exactly as a real cold launch does.
    const rehydrate = deferred<{ items: unknown[] }>();
    mockGetTracks.mockReturnValue(rehydrate.promise);

    renderHook(() => useQueueResume());
    await waitFor(() => expect(mockGetTracks).toHaveBeenCalled());

    // The user taps a track while the rehydrate is still in flight.
    useQueueStore.getState().loadQueue(
      [
        {
          source: { kind: 'library', trackId: 'user-pick' },
          title: 'user-pick',
          artist: 'a',
          artworkUrl: null,
        },
      ],
      0,
      null,
    );
    const userGeneration = useQueueStore.getState().generation;

    rehydrate.resolve({ items: [savedTrack('saved-1'), savedTrack('saved-2')] });
    await waitFor(() => expect(mockGetTracks).toHaveBeenCalledTimes(1));

    // The user's queue survives untouched, and the native player is never primed
    // with the saved queue (which would reset() their audio).
    expect(useQueueStore.getState().generation).toBe(userGeneration);
    expect(useQueueStore.getState().currentTrack()?.title).toBe('user-pick');
    expect(mockLoadNativeQueue).not.toHaveBeenCalled();
  });

  it('restores normally when the user does nothing', async () => {
    mockGetQueueState.mockResolvedValue({
      track_ids: ['saved-1', 'saved-2'],
      natural_order: ['saved-1', 'saved-2'],
      current_index: 0,
      position_ms: 90_000,
      shuffled: false,
      repeat_mode: 'off',
      source_id: 'library',
      current_track: null,
    });
    mockGetTracks.mockResolvedValue({ items: [savedTrack('saved-1'), savedTrack('saved-2')] });

    renderHook(() => useQueueResume());

    await waitFor(() => expect(mockLoadNativeQueue).toHaveBeenCalled());
    expect(useQueueStore.getState().tracks).toHaveLength(2);
    expect(useQueueStore.getState().currentTrack()?.title).toBe('saved-1');
  });
});

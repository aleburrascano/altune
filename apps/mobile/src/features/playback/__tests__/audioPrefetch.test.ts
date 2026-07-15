/**
 * prefetchNext swaps the *upcoming* track's native queue entry to a local file.
 * It must never touch the playing track: RNTP activates the next track when the
 * active one is removed, so a mistimed swap causes an audible extra skip.
 *
 * Local track-player mock with stable jest.fns (the shared manual mock returns
 * a fresh fn per access).
 */

import type { PlaybackTrack } from '@shared/playback/types';

jest.mock('react-native-track-player', () => ({
  __esModule: true,
  default: {
    remove: jest.fn().mockResolvedValue(undefined),
    add: jest.fn().mockResolvedValue(undefined),
    load: jest.fn().mockResolvedValue(undefined),
    play: jest.fn().mockResolvedValue(undefined),
    getActiveTrackIndex: jest.fn().mockResolvedValue(0),
  },
}));

// Cache dir + downloads are filesystem work; the swap path under test only needs
// findCached to report a hit, so point every lookup at one already-local file.
jest.mock('expo-file-system', () => ({
  Paths: { cache: '/cache' },
  Directory: class {
    exists = true;
    create() {}
    list() {
      return [];
    }
  },
  File: class {},
}));

jest.mock('../api/audio', () => ({
  fetchAudioUrls: jest.fn().mockResolvedValue([]),
  audioRequestHeaders: jest.fn().mockResolvedValue({ Authorization: 'Bearer t' }),
  audioStreamUrl: (id: string) => `https://api.test/v1/tracks/${id}/audio`,
}));

import TrackPlayer from 'react-native-track-player';
import { useQueueStore } from '@shared/playback/queueStore';
import {
  forgetAllSwaps,
  repairActiveToStreaming,
  swapUpcomingToLocal,
  wasSwappedToLocal,
} from '../audioPrefetch';

const mockTrackPlayer = TrackPlayer as unknown as {
  remove: jest.Mock;
  add: jest.Mock;
  load: jest.Mock;
  play: jest.Mock;
  getActiveTrackIndex: jest.Mock;
};

function libraryTrack(id: string): PlaybackTrack {
  return {
    source: { kind: 'library', trackId: id },
    title: id,
    artist: `${id}-artist`,
    artworkUrl: null,
  };
}

describe('prefetchNext queue swap', () => {
  beforeEach(() => {
    jest.clearAllMocks();
    forgetAllSwaps();
    useQueueStore.getState().loadQueue([libraryTrack('a'), libraryTrack('b'), libraryTrack('c')], 0, null);
  });

  // The race: the PlaybackActiveTrackChanged(0) event schedules a prefetch of
  // index 1, then the user presses next before the swap runs — index 1 is now
  // the PLAYING track. Removing it would make RNTP activate index 2, skipping a
  // track the user never asked to skip.
  it('does not remove the track that is now playing', async () => {
    // Native has already advanced to index 1 by the time the swap runs.
    mockTrackPlayer.getActiveTrackIndex.mockResolvedValue(1);

    await swapUpcomingToLocal(1, libraryTrack('b'), 'file:///cache/b.mp3');

    expect(mockTrackPlayer.remove).not.toHaveBeenCalled();
  });

  it('swaps a genuinely upcoming track', async () => {
    mockTrackPlayer.getActiveTrackIndex.mockResolvedValue(0);

    await swapUpcomingToLocal(1, libraryTrack('b'), 'file:///cache/b.mp3');

    expect(mockTrackPlayer.remove).toHaveBeenCalledWith(1);
    expect(mockTrackPlayer.add).toHaveBeenCalledTimes(1);
    expect(mockTrackPlayer.add.mock.calls[0]![1]).toBe(1);
  });

  // remove+add is not atomic. Losing the slot would offset every later native
  // index from the store's play order permanently and silently.
  it('puts a streaming entry back when the local re-add fails', async () => {
    mockTrackPlayer.getActiveTrackIndex.mockResolvedValue(0);
    mockTrackPlayer.add
      .mockRejectedValueOnce(new Error('add failed'))
      .mockResolvedValueOnce(undefined);

    await swapUpcomingToLocal(1, libraryTrack('b'), 'file:///cache/b.mp3');

    expect(mockTrackPlayer.add).toHaveBeenCalledTimes(2);
    const restored = mockTrackPlayer.add.mock.calls[1]!;
    expect(restored[1]).toBe(1); // same slot
    expect(String(restored[0].url)).toContain('/v1/tracks/b/audio'); // streamable again
    expect(wasSwappedToLocal('b')).toBe(false);
  });
});

describe('stale local cache recovery', () => {
  beforeEach(() => {
    jest.clearAllMocks();
    forgetAllSwaps();
    useQueueStore.getState().loadQueue([libraryTrack('a'), libraryTrack('b')], 0, null);
  });

  it('remembers which tracks point at a local file', async () => {
    mockTrackPlayer.getActiveTrackIndex.mockResolvedValue(0);
    expect(wasSwappedToLocal('b')).toBe(false);

    await swapUpcomingToLocal(1, libraryTrack('b'), 'file:///cache/b.mp3');

    expect(wasSwappedToLocal('b')).toBe(true);
  });

  it('forgets swaps when the queue is rebuilt from streaming URLs', async () => {
    mockTrackPlayer.getActiveTrackIndex.mockResolvedValue(0);
    await swapUpcomingToLocal(1, libraryTrack('b'), 'file:///cache/b.mp3');

    forgetAllSwaps();

    expect(wasSwappedToLocal('b')).toBe(false);
  });

  // Repairing the ACTIVE entry must use load() — remove+add would reindex the
  // queue out from under the store.
  it('repairs the active entry in place, without touching queue indexes', async () => {
    mockTrackPlayer.getActiveTrackIndex.mockResolvedValue(0);
    await swapUpcomingToLocal(1, libraryTrack('b'), 'file:///cache/b.mp3');
    jest.clearAllMocks();

    await repairActiveToStreaming(libraryTrack('b'));

    expect(mockTrackPlayer.load).toHaveBeenCalledTimes(1);
    expect(String(mockTrackPlayer.load.mock.calls[0]![0].url)).toContain('/v1/tracks/b/audio');
    expect(mockTrackPlayer.remove).not.toHaveBeenCalled();
    expect(mockTrackPlayer.play).toHaveBeenCalled();
    // No longer local — a later failure is a real one and must reach the server.
    expect(wasSwappedToLocal('b')).toBe(false);
  });
});

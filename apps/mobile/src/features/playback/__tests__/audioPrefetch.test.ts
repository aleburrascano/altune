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
}));

import TrackPlayer from 'react-native-track-player';
import { useQueueStore } from '@shared/playback/queueStore';
import { swapUpcomingToLocal } from '../audioPrefetch';

const mockTrackPlayer = TrackPlayer as unknown as {
  remove: jest.Mock;
  add: jest.Mock;
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
});

import type { PlaybackTrack } from '@shared/playback/types';

jest.mock('react-native-track-player', () => ({
  __esModule: true,
  default: {
    remove: jest.fn().mockResolvedValue(undefined),
    add: jest.fn().mockResolvedValue(undefined),
    load: jest.fn().mockResolvedValue(undefined),
    play: jest.fn().mockResolvedValue(undefined),
    getActiveTrackIndex: jest.fn().mockResolvedValue(0),
    getActiveTrack: jest.fn().mockResolvedValue(undefined),
    getQueue: jest.fn().mockResolvedValue([]),
  },
}));

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

jest.mock('@shared/api-client/audio', () => ({
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
import { usePlaybackErrorStore } from '../playbackErrorStore';

const mockTrackPlayer = TrackPlayer as unknown as {
  remove: jest.Mock;
  add: jest.Mock;
  load: jest.Mock;
  play: jest.Mock;
  getActiveTrackIndex: jest.Mock;
  getActiveTrack: jest.Mock;
  getQueue: jest.Mock;
};

function libraryTrack(id: string): PlaybackTrack {
  return {
    source: { kind: 'library', trackId: id },
    title: id,
    artist: `${id}-artist`,
    artworkUrl: null,
  };
}

function nativeQueue(...ids: string[]): { id: string }[] {
  return ids.map((id) => ({ id: `library:${id}` }));
}

describe('prefetchNext queue swap', () => {
  beforeEach(() => {
    jest.clearAllMocks();
    forgetAllSwaps();
    mockTrackPlayer.getQueue.mockResolvedValue(nativeQueue('a', 'b', 'c'));
    useQueueStore
      .getState()
      .loadQueue([libraryTrack('a'), libraryTrack('b'), libraryTrack('c')], 0, null);
  });

  it('does not remove the track that is now playing', async () => {
    mockTrackPlayer.getActiveTrackIndex.mockResolvedValue(1);

    await swapUpcomingToLocal(libraryTrack('b'), 'file:///cache/b.mp3');

    expect(mockTrackPlayer.remove).not.toHaveBeenCalled();
  });

  it('swaps a genuinely upcoming track', async () => {
    mockTrackPlayer.getActiveTrackIndex.mockResolvedValue(0);

    await swapUpcomingToLocal(libraryTrack('b'), 'file:///cache/b.mp3');

    expect(mockTrackPlayer.remove).toHaveBeenCalledWith(1);
    expect(mockTrackPlayer.add).toHaveBeenCalledTimes(1);
    expect(mockTrackPlayer.add.mock.calls[0]![1]).toBe(1);
  });

  it('targets the slot the track actually occupies, not the store position', async () => {
    mockTrackPlayer.getActiveTrackIndex.mockResolvedValue(0);
    mockTrackPlayer.getQueue.mockResolvedValue(nativeQueue('a', 'x', 'b'));

    await swapUpcomingToLocal(libraryTrack('b'), 'file:///cache/b.mp3');

    expect(mockTrackPlayer.remove).toHaveBeenCalledWith(2);
    expect(mockTrackPlayer.add.mock.calls[0]![1]).toBe(2);
  });

  it('does nothing when the track is no longer in the native queue', async () => {
    mockTrackPlayer.getActiveTrackIndex.mockResolvedValue(0);
    mockTrackPlayer.getQueue.mockResolvedValue(nativeQueue('a', 'c'));

    await swapUpcomingToLocal(libraryTrack('b'), 'file:///cache/b.mp3');

    expect(mockTrackPlayer.remove).not.toHaveBeenCalled();
    expect(mockTrackPlayer.add).not.toHaveBeenCalled();
  });

  it('puts a streaming entry back when the local re-add fails', async () => {
    mockTrackPlayer.getActiveTrackIndex.mockResolvedValue(0);
    mockTrackPlayer.add
      .mockRejectedValueOnce(new Error('add failed'))
      .mockResolvedValueOnce(undefined);

    await swapUpcomingToLocal(libraryTrack('b'), 'file:///cache/b.mp3');

    expect(mockTrackPlayer.add).toHaveBeenCalledTimes(2);
    const restored = mockTrackPlayer.add.mock.calls[1]!;
    expect(restored[1]).toBe(1);
    expect(String(restored[0].url)).toContain('/v1/tracks/b/audio');
    expect(wasSwappedToLocal('b')).toBe(false);
  });

  it('reports a track-scoped error when the slot cannot be refilled at all', async () => {
    mockTrackPlayer.getActiveTrackIndex.mockResolvedValue(0);
    mockTrackPlayer.add
      .mockRejectedValueOnce(new Error('add failed'))
      .mockRejectedValueOnce(new Error('add failed'));

    await swapUpcomingToLocal(libraryTrack('b'), 'file:///cache/b.mp3');

    expect(usePlaybackErrorStore.getState().key).toBe('library:b');
  });
});

describe('stale local cache recovery', () => {
  beforeEach(() => {
    jest.clearAllMocks();
    forgetAllSwaps();
    mockTrackPlayer.getQueue.mockResolvedValue(nativeQueue('a', 'b'));
    useQueueStore.getState().loadQueue([libraryTrack('a'), libraryTrack('b')], 0, null);
  });

  it('remembers which tracks point at a local file', async () => {
    mockTrackPlayer.getActiveTrackIndex.mockResolvedValue(0);
    expect(wasSwappedToLocal('b')).toBe(false);

    await swapUpcomingToLocal(libraryTrack('b'), 'file:///cache/b.mp3');

    expect(wasSwappedToLocal('b')).toBe(true);
  });

  it('forgets swaps when the queue is rebuilt from streaming URLs', async () => {
    mockTrackPlayer.getActiveTrackIndex.mockResolvedValue(0);
    await swapUpcomingToLocal(libraryTrack('b'), 'file:///cache/b.mp3');

    forgetAllSwaps();

    expect(wasSwappedToLocal('b')).toBe(false);
  });

  it('repairs the active entry in place, without touching queue indexes', async () => {
    mockTrackPlayer.getActiveTrackIndex.mockResolvedValue(0);
    await swapUpcomingToLocal(libraryTrack('b'), 'file:///cache/b.mp3');
    jest.clearAllMocks();
    mockTrackPlayer.getActiveTrack.mockResolvedValue({ id: 'library:b' });

    await repairActiveToStreaming(libraryTrack('b'));

    expect(mockTrackPlayer.load).toHaveBeenCalledTimes(1);
    expect(String(mockTrackPlayer.load.mock.calls[0]![0].url)).toContain('/v1/tracks/b/audio');
    expect(mockTrackPlayer.remove).not.toHaveBeenCalled();
    expect(mockTrackPlayer.play).toHaveBeenCalled();
    expect(wasSwappedToLocal('b')).toBe(false);
  });

  it('leaves a healthy active track alone when the failure was another track', async () => {
    mockTrackPlayer.getActiveTrackIndex.mockResolvedValue(0);
    await swapUpcomingToLocal(libraryTrack('b'), 'file:///cache/b.mp3');
    jest.clearAllMocks();
    mockTrackPlayer.getActiveTrack.mockResolvedValue({ id: 'library:a' });

    await repairActiveToStreaming(libraryTrack('b'));

    expect(mockTrackPlayer.load).not.toHaveBeenCalled();
    expect(mockTrackPlayer.play).not.toHaveBeenCalled();
  });
});

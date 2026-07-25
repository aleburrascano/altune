import type { PlaybackTrack } from '@shared/playback/types';

const mockHandlers: Record<string, (...args: unknown[]) => unknown> = {};

jest.mock('react-native-track-player', () => ({
  __esModule: true,
  default: {
    addEventListener: jest.fn((event: string, cb: (...a: unknown[]) => unknown) => {
      mockHandlers[event] = cb;
      return { remove: jest.fn() };
    }),
    getProgress: jest.fn().mockResolvedValue({ position: 0, duration: 0, buffered: 0 }),
    getActiveTrack: jest.fn().mockResolvedValue(undefined),
    seekTo: jest.fn(),
    skipToPrevious: jest.fn(),
    skipToNext: jest.fn(),
    pause: jest.fn(),
    play: jest.fn(),
  },
  Event: {
    RemotePause: 'remote-pause',
    RemotePlay: 'remote-play',
    RemoteNext: 'remote-next',
    RemotePrevious: 'remote-previous',
    RemoteSeek: 'remote-seek',
    PlaybackActiveTrackChanged: 'playback-active-track-changed',
    PlaybackError: 'playback-error',
  },
}));

jest.mock('@shared/api-client/audio', () => ({
  recoverAudio: jest.fn().mockResolvedValue(undefined),
  audioRequestHeaders: jest.fn().mockResolvedValue({}),
  fetchAudioUrls: jest.fn().mockResolvedValue([]),
  audioStreamUrl: (id: string) => `https://api.test/v1/tracks/${id}/audio`,
}));

jest.mock('../audioPrefetch', () => ({
  prefetchNext: jest.fn().mockResolvedValue(undefined),
  repairActiveToStreaming: jest.fn().mockResolvedValue(undefined),
  wasSwappedToLocal: jest.fn().mockReturnValue(false),
}));

import TrackPlayer, { Event } from 'react-native-track-player';
import { recoverAudio } from '@shared/api-client/audio';
import { useQueueStore } from '@shared/playback/queueStore';
import { playbackService } from '../service';
import { usePlaybackErrorStore } from '../playbackErrorStore';

const tp = TrackPlayer as unknown as { getActiveTrack: jest.Mock };
const mockRecoverAudio = recoverAudio as jest.Mock;

function libraryTrack(id: string): PlaybackTrack {
  return {
    source: { kind: 'library', trackId: id },
    title: id,
    artist: `${id}-artist`,
    artworkUrl: null,
  };
}

describe('PlaybackError attribution', () => {
  beforeEach(async () => {
    jest.clearAllMocks();
    for (const key of Object.keys(mockHandlers)) delete mockHandlers[key];
    usePlaybackErrorStore.getState().clear();
    useQueueStore
      .getState()
      .loadQueue([libraryTrack('a'), libraryTrack('b'), libraryTrack('c')], 0, null);
    await playbackService();
  });

  it('blames the track the player is actually on, not the store index', async () => {
    useQueueStore.getState().syncCurrentIndex(2);
    tp.getActiveTrack.mockResolvedValue({ id: 'library:a' });

    await mockHandlers[Event.PlaybackError]!({ code: 'x', message: 'boom' });

    expect(usePlaybackErrorStore.getState().key).toBe('library:a');
    expect(mockRecoverAudio).toHaveBeenCalledWith('a');
  });

  it('surfaces the error message so the player can leave loading', async () => {
    tp.getActiveTrack.mockResolvedValue({ id: 'library:b' });

    await mockHandlers[Event.PlaybackError]!({ code: 'x', message: 'network unreachable' });

    expect(usePlaybackErrorStore.getState().message).toBe('network unreachable');
  });

  it('falls back to the store when the native track carries no identity', async () => {
    tp.getActiveTrack.mockResolvedValue(undefined);

    await mockHandlers[Event.PlaybackError]!({ code: 'x', message: 'boom' });

    expect(usePlaybackErrorStore.getState().key).toBe('library:a');
    expect(mockRecoverAudio).toHaveBeenCalledWith('a');
  });
});

describe('PlaybackActiveTrackChanged reconciliation', () => {
  beforeEach(async () => {
    jest.clearAllMocks();
    for (const key of Object.keys(mockHandlers)) delete mockHandlers[key];
    useQueueStore
      .getState()
      .loadQueue([libraryTrack('a'), libraryTrack('b'), libraryTrack('c')], 0, null);
    await playbackService();
  });

  it('follows the track identity when the native index has drifted', () => {
    mockHandlers[Event.PlaybackActiveTrackChanged]!({ index: 1, track: { id: 'library:c' } });

    expect(useQueueStore.getState().currentIndex).toBe(2);
  });

  it('uses the reported index when it agrees with the identity', () => {
    mockHandlers[Event.PlaybackActiveTrackChanged]!({ index: 1, track: { id: 'library:b' } });

    expect(useQueueStore.getState().currentIndex).toBe(1);
  });
});

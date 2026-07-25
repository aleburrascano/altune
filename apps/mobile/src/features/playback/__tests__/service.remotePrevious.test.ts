const mockHandlers: Record<string, (...args: unknown[]) => unknown> = {};

jest.mock('react-native-track-player', () => ({
  __esModule: true,
  default: {
    addEventListener: jest.fn((event: string, cb: (...a: unknown[]) => unknown) => {
      mockHandlers[event] = cb;
      return { remove: jest.fn() };
    }),
    getProgress: jest.fn(),
    seekTo: jest.fn().mockResolvedValue(undefined),
    skipToPrevious: jest.fn().mockResolvedValue(undefined),
    skipToNext: jest.fn().mockResolvedValue(undefined),
    pause: jest.fn().mockResolvedValue(undefined),
    play: jest.fn().mockResolvedValue(undefined),
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

jest.mock('@shared/playback/queueStore', () => ({
  useQueueStore: { getState: () => ({ syncCurrentIndex: jest.fn(), currentTrack: () => null }) },
  orderedQueueTracks: () => [],
}));

import TrackPlayer, { Event } from 'react-native-track-player';
import { playbackService } from '../service';

const tp = TrackPlayer as unknown as {
  getProgress: jest.Mock;
  seekTo: jest.Mock;
  skipToPrevious: jest.Mock;
};

describe('RemotePrevious (lock-screen previous)', () => {
  beforeEach(async () => {
    jest.clearAllMocks();
    for (const key of Object.keys(mockHandlers)) delete mockHandlers[key];
    await playbackService();
  });

  it('restarts the current track when more than 3s in', async () => {
    tp.getProgress.mockResolvedValue({ position: 90, duration: 200, buffered: 0 });

    await mockHandlers[Event.RemotePrevious]!();

    expect(tp.seekTo).toHaveBeenCalledWith(0);
    expect(tp.skipToPrevious).not.toHaveBeenCalled();
  });

  it('steps back a track when near the start (under 3s)', async () => {
    tp.getProgress.mockResolvedValue({ position: 1, duration: 200, buffered: 0 });

    await mockHandlers[Event.RemotePrevious]!();

    expect(tp.skipToPrevious).toHaveBeenCalledTimes(1);
    expect(tp.seekTo).not.toHaveBeenCalled();
  });
});

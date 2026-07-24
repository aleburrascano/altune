jest.mock('react-native-track-player', () => ({
  __esModule: true,
  default: {
    seekTo: jest.fn().mockResolvedValue(undefined),
    play: jest.fn().mockResolvedValue(undefined),
  },
}));

import TrackPlayer from 'react-native-track-player';
import { seekPreservingPlayback } from '../seekControls';

const tp = TrackPlayer as unknown as { seekTo: jest.Mock; play: jest.Mock };

describe('seekPreservingPlayback', () => {
  beforeEach(() => jest.clearAllMocks());

  it('re-asserts play after seeking when the track was playing', async () => {
    await seekPreservingPlayback(42, true);

    expect(tp.seekTo).toHaveBeenCalledWith(42);
    expect(tp.play).toHaveBeenCalledTimes(1);
    expect(tp.seekTo.mock.invocationCallOrder[0]!).toBeLessThan(
      tp.play.mock.invocationCallOrder[0]!,
    );
  });

  it('does not start playback when the track was paused', async () => {
    await seekPreservingPlayback(42, false);

    expect(tp.seekTo).toHaveBeenCalledWith(42);
    expect(tp.play).not.toHaveBeenCalled();
  });
});

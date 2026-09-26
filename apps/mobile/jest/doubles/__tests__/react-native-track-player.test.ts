import TrackPlayer, { __player } from 'react-native-track-player';

describe('track-player double', () => {
  it('exposes a stable mock per method, so call assertions hold', async () => {
    expect(TrackPlayer.play).toBe(TrackPlayer.play);

    await TrackPlayer.play();

    expect(TrackPlayer.play).toHaveBeenCalledTimes(1);
  });

  it('injects a rejection on a named method', async () => {
    __player.failNext('skipToNext', new Error('player not initialised'));
    await expect(TrackPlayer.skipToNext()).rejects.toThrow('player not initialised');
  });

  it('drives playback state and progress', () => {
    __player.setState('playing');
    __player.setProgress({ position: 42, duration: 180 });

    const { usePlaybackState, useProgress } = require('react-native-track-player');

    expect(usePlaybackState().state).toBe('playing');
    expect(useProgress()).toEqual({ position: 42, duration: 180, buffered: 0 });
  });

  it('resets call counts between tests', () => {
    expect(TrackPlayer.play).toHaveBeenCalledTimes(0);
  });
});

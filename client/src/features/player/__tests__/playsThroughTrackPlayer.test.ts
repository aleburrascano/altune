import { isExpoGo, playsThroughTrackPlayer } from '@features/player';
import { playsThroughTrackPlayer as playsThroughTrackPlayerWeb } from '@features/player/domain/playsThroughTrackPlayer.web';

describe('playsThroughTrackPlayer', () => {
  it('holds natively exactly when the app is not running in Expo Go', () => {
    expect(playsThroughTrackPlayer).toBe(!isExpoGo);
  });

  it('is false on web', () => {
    expect(playsThroughTrackPlayerWeb).toBe(false);
  });
});

import { isExpoGo, playsThroughTrackPlayer } from '@features/player';

describe('playsThroughTrackPlayer', () => {
  it('holds natively exactly when the app is not running in Expo Go', () => {
    expect(playsThroughTrackPlayer).toBe(!isExpoGo);
  });
});

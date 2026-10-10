import { isExpoGo } from '@features/playback/domain/isExpoGo';

import { playsThroughTrackPlayer } from '@features/playback/domain/playsThroughTrackPlayer';
import { playsThroughTrackPlayer as playsThroughTrackPlayerWeb } from '@features/playback/domain/playsThroughTrackPlayer.web';

describe('playsThroughTrackPlayer', () => {
  it('holds natively exactly when the app is not running in Expo Go', () => {
    expect(playsThroughTrackPlayer).toBe(!isExpoGo);
  });

  it('is false on web', () => {
    expect(playsThroughTrackPlayerWeb).toBe(false);
  });
});

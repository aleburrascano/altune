import { isExpoGo } from '@shared/playback/isExpoGo';

import { playsThroughTrackPlayer } from '../playsThroughTrackPlayer';
import { playsThroughTrackPlayer as playsThroughTrackPlayerWeb } from '../playsThroughTrackPlayer.web';

describe('playsThroughTrackPlayer', () => {
  it('holds natively exactly when the app is not running in Expo Go', () => {
    expect(playsThroughTrackPlayer).toBe(!isExpoGo);
  });

  it('is false on web', () => {
    expect(playsThroughTrackPlayerWeb).toBe(false);
  });
});

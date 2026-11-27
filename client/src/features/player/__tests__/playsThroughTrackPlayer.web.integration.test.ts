import { playsThroughTrackPlayer } from '@features/player';

describe('playsThroughTrackPlayer on web', () => {
  it('is false', () => {
    expect(playsThroughTrackPlayer).toBe(false);
  });
});

import { endedLabel, statusFlagsOf } from '../playerStatus';

describe('playerStatus', () => {
  it('labels an ended preview and an ended full track differently', () => {
    expect(endedLabel(true)).toBe('Preview ended');
    expect(endedLabel(false)).toBe('Finished');
  });

  it('derives exactly one flag from the status', () => {
    expect(statusFlagsOf('playing')).toEqual({ isPlaying: true, isEnded: false, isError: false });
    expect(statusFlagsOf('ended')).toEqual({ isPlaying: false, isEnded: true, isError: false });
    expect(statusFlagsOf('error')).toEqual({ isPlaying: false, isEnded: false, isError: true });
  });
});

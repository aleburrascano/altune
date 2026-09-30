import { progressSecondsToMs, progressSecondsToRoundedMs } from '../time';

describe('progressSecondsToMs — native TrackPlayer seconds to milliseconds', () => {
  it('keeps sub-millisecond precision', () => {
    expect(progressSecondsToMs(1.2345)).toBeCloseTo(1234.5, 6);
  });

  it('converts whole seconds exactly', () => {
    expect(progressSecondsToMs(3)).toBe(3000);
  });
});

describe('progressSecondsToRoundedMs — native TrackPlayer seconds to whole milliseconds', () => {
  it('rounds to the nearest millisecond', () => {
    expect(progressSecondsToRoundedMs(1.2346)).toBe(1235);
    expect(progressSecondsToRoundedMs(1.2344)).toBe(1234);
  });

  it('returns zero for zero', () => {
    expect(progressSecondsToRoundedMs(0)).toBe(0);
  });
});

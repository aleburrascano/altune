import {
  msToSeconds,
  progressSecondsToMs,
  progressSecondsToRoundedMs,
  secondsToMs,
} from '@features/playback/domain/time';

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

const ROUNDS_UP_SECONDS = 1.2346;
const ROUNDED_UP_MS = 1235;
const ROUNDS_DOWN_SECONDS = 1.2344;
const ROUNDED_DOWN_MS = 1234;
const WHOLE_MS = 1234;
const WHOLE_MS_IN_SECONDS = 1.234;

describe('secondsToMs — media element seconds to whole milliseconds', () => {
  it('rounds to the nearest millisecond', () => {
    expect(secondsToMs(ROUNDS_UP_SECONDS)).toBe(ROUNDED_UP_MS);
    expect(secondsToMs(ROUNDS_DOWN_SECONDS)).toBe(ROUNDED_DOWN_MS);
  });

  it('returns zero for a non-finite reading', () => {
    expect(secondsToMs(NaN)).toBe(0);
    expect(secondsToMs(Infinity)).toBe(0);
  });
});

describe('msToSeconds — milliseconds to seconds', () => {
  it('divides without rounding', () => {
    expect(msToSeconds(WHOLE_MS)).toBe(WHOLE_MS_IN_SECONDS);
    expect(msToSeconds(0)).toBe(0);
  });

  it('passes a non-finite value through unguarded', () => {
    expect(msToSeconds(Infinity)).toBe(Infinity);
  });
});

import fc from 'fast-check';

import { formatDuration } from '../format';

describe('formatDuration — boundaries the minutes/seconds arithmetic turns on', () => {
  it.each<[number, string]>([
    [0, '0:00'],
    [59, '0:59'],
    [60, '1:00'],
    [61, '1:01'],
    [599, '9:59'],
    [600, '10:00'],
    [3599, '59:59'],
    [3600, '60:00'],
  ])('formats %i seconds as %s', (totalSeconds, expected) => {
    expect(formatDuration(totalSeconds)).toBe(expected);
  });

  it('floors a non-integer number of seconds before splitting minutes/seconds', () => {
    expect(formatDuration(65.7)).toBe('1:05');
  });
});

describe('formatDuration — a Track duration renders as the m:ss the app promises', () => {
  it('renders a four-minute-thirty-two-second Track as "4:32"', () => {
    expect(formatDuration(272)).toBe('4:32');
  });
});

describe('law: formatDuration over any finite input, negative included', () => {
  it('always produces an m:ss string with zero-padded, sub-60 seconds', () => {
    fc.assert(
      fc.property(
        fc.double({ min: -1_000_000, max: 1_000_000, noNaN: true, noDefaultInfinity: true }),
        (totalSeconds) => {
          expect(formatDuration(totalSeconds)).toMatch(/^\d+:[0-5]\d$/);
        },
      ),
    );
  });

  it('reconstructs the clamped floored input from its own minutes*60 + seconds', () => {
    fc.assert(
      fc.property(
        fc.double({ min: -1_000_000, max: 1_000_000, noNaN: true, noDefaultInfinity: true }),
        (totalSeconds) => {
          const parts = formatDuration(totalSeconds).split(':').map(Number);
          expect(parts[0]! * 60 + parts[1]!).toBe(Math.max(0, Math.floor(totalSeconds)));
        },
      ),
    );
  });
});

describe('formatDuration — a negative duration clamps to zero rather than rendering "-1:-5"', () => {
  it.each<[number, string]>([
    [-1, '0:00'],
    [-5, '0:00'],
    [-59, '0:00'],
    [-60, '0:00'],
    [-3600, '0:00'],
  ])('renders %i seconds as %s', (totalSeconds, expected) => {
    expect(formatDuration(totalSeconds)).toBe(expected);
  });

  it('reaches the clamp through the two call sites that guard only against null', () => {
    const fromTrackExtras = -12;
    expect(formatDuration(fromTrackExtras)).toMatch(/^\d+:[0-5]\d$/);
  });
});

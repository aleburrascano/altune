import { formatDuration } from '../format';

describe('formatDuration', () => {
  it('formats zero seconds', () => {
    expect(formatDuration(0)).toBe('0:00');
  });

  it('formats seconds under a minute', () => {
    expect(formatDuration(45)).toBe('0:45');
  });

  it('pads single-digit seconds', () => {
    expect(formatDuration(63)).toBe('1:03');
  });

  it('formats exactly one minute', () => {
    expect(formatDuration(60)).toBe('1:00');
  });

  it('formats multi-minute tracks', () => {
    expect(formatDuration(195)).toBe('3:15');
  });

  it('formats long tracks (over an hour)', () => {
    expect(formatDuration(3661)).toBe('61:01');
  });

  it('truncates fractional seconds', () => {
    expect(formatDuration(90.7)).toBe('1:30');
  });
});

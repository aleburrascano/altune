import { clampedExponent, equalJitterMs } from '../backoff';

describe('clampedExponent', () => {
  it.each([
    [-3, 0, 0],
    [4, 0, 4],
    [0, 1, 1],
    [99, 1, 30],
  ])('attempt %d with floor %d gives %d', (attempt, floor, expected) => {
    expect(clampedExponent(attempt, floor)).toBe(expected);
  });
});

describe('equalJitterMs', () => {
  it('spans half the ceiling to the full ceiling', () => {
    expect(equalJitterMs(1_000, 30_000, 2, 0)).toBe(2_000);
    expect(equalJitterMs(1_000, 30_000, 2, 1)).toBe(4_000);
  });

  it('never exceeds the cap', () => {
    expect(equalJitterMs(1_000, 30_000, 30, 1)).toBe(30_000);
  });
});

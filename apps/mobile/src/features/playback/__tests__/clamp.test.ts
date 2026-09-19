import { clamp } from '../clamp';

describe('clamp — holding a value inside its bounds', () => {
  it('returns the value when it is already inside the bounds', () => {
    expect(clamp(3, 0, 10)).toBe(3);
  });

  it('returns the lower bound when the value is below it', () => {
    expect(clamp(-2, 0, 10)).toBe(0);
  });

  it('returns the upper bound when the value is above it', () => {
    expect(clamp(42, 0, 10)).toBe(10);
  });

  it('keeps both bounds themselves', () => {
    expect(clamp(0, 0, 10)).toBe(0);
    expect(clamp(10, 0, 10)).toBe(10);
  });

  it('returns the lower bound when the upper one is below it (an empty list index)', () => {
    expect(clamp(5, 0, -1)).toBe(0);
  });
});

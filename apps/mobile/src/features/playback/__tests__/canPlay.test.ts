import { canPlay } from '../helpers/canPlay';

describe('canPlay', () => {
  it('returns true for ready status', () => {
    expect(canPlay('ready')).toBe(true);
  });

  it('returns false for pending status', () => {
    expect(canPlay('pending')).toBe(false);
  });

  it('returns false for failed status', () => {
    expect(canPlay('failed')).toBe(false);
  });

  it('returns false for null', () => {
    expect(canPlay(null)).toBe(false);
  });

  it('returns false for undefined', () => {
    expect(canPlay(undefined)).toBe(false);
  });
});

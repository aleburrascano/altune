import { normalizeForCompare } from '../text-compare';

describe('normalizeForCompare', () => {
  it('matches two titles differing only in case and surrounding whitespace', () => {
    expect(normalizeForCompare('  The Blue Album ')).toBe(normalizeForCompare('the blue album'));
  });

  it('keeps whitespace inside the value, so two words never collapse into one', () => {
    expect(normalizeForCompare('Blue Album')).toBe('blue album');
  });

  it('folds a missing value to the empty string', () => {
    expect(normalizeForCompare(null)).toBe('');
  });

  it('matches a missing value with a blank one', () => {
    expect(normalizeForCompare(null)).toBe(normalizeForCompare('   '));
  });

  it('never matches a missing value with a real title', () => {
    expect(normalizeForCompare(null)).not.toBe(normalizeForCompare('Blue Album'));
  });
});

import {
  DEFAULT_PASSWORD_MIN_LENGTH,
  isValidEmail,
  passwordsMatch,
  validatePassword,
} from '../validation';

describe('isValidEmail', () => {
  it.each([
    'you@email.com',
    'a.b-c+tag@sub.domain.io',
    'x@y.co',
  ])('accepts %s', (email) => {
    expect(isValidEmail(email)).toBe(true);
  });

  it.each([
    '',
    'plainstring',
    'no@domain',
    '@nolocal.com',
    'spaces in@email.com',
    'two@@at.com',
  ])('rejects %s', (email) => {
    expect(isValidEmail(email)).toBe(false);
  });

  it('trims surrounding whitespace before validating', () => {
    expect(isValidEmail('  you@email.com  ')).toBe(true);
  });
});

describe('validatePassword', () => {
  it('returns no issues for a password meeting every rule', () => {
    expect(validatePassword('Longenough1!')).toEqual([]);
  });

  it('flags too_short below the default minimum', () => {
    expect(validatePassword('Ab1!')).toContain('too_short');
  });

  it('flags each missing character class (mirrors the Supabase policy)', () => {
    expect(validatePassword('alllowercase1!')).toContain('no_uppercase');
    expect(validatePassword('ALLUPPERCASE1!')).toContain('no_lowercase');
    expect(validatePassword('NoNumbersHere!')).toContain('no_number');
    expect(validatePassword('NoSymbolsHere1')).toContain('no_symbol');
  });

  it('respects a custom minimum length while still enforcing classes', () => {
    expect(validatePassword('Ab1!xy', 4)).toEqual([]);
    expect(validatePassword('A1!', 4)).toContain('too_short');
  });

  it('exposes the default minimum as a constant', () => {
    expect(DEFAULT_PASSWORD_MIN_LENGTH).toBe(8);
  });
});

describe('passwordsMatch', () => {
  it('is true only when both are non-empty and equal', () => {
    expect(passwordsMatch('secret123', 'secret123')).toBe(true);
  });

  it('is false when they differ', () => {
    expect(passwordsMatch('secret123', 'secret124')).toBe(false);
  });

  it('is false when the confirm field is empty', () => {
    expect(passwordsMatch('secret123', '')).toBe(false);
  });
});

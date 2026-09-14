import { isContentError } from '../content-status';

describe('isContentError', () => {
  it('is an error when the query itself failed, whatever the response', () => {
    expect(isContentError(true, undefined)).toBe(true);
    expect(isContentError(true, { status: 'ok' })).toBe(true);
  });

  it.each(['timeout', 'rate_limited', 'circuit_open', 'error'] as const)(
    'treats a %s provider status as an error',
    (status) => {
      expect(isContentError(false, { status })).toBe(true);
    },
  );

  it('is not an error for a healthy response', () => {
    expect(isContentError(false, { status: 'ok' })).toBe(false);
  });

  it('is not an error before a response arrives', () => {
    expect(isContentError(false, undefined)).toBe(false);
    expect(isContentError(false, null)).toBe(false);
  });
});

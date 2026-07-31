import { isNetworkError } from '../isNetworkError';

describe('isNetworkError — each regex alternate, independently', () => {
  it.each([
    ['a network error', true],
    ['fetch failed', true],
    ['request timeout', true],
    ['connection refused', true],
    ['unauthorized', false],
    ['invalid credentials', false],
    ['', false],
  ])('Error(%j) -> %s', (message, expected) => {
    expect(isNetworkError(new Error(message as string))).toBe(expected);
  });

  it('matches regardless of case, for every alternate', () => {
    expect(isNetworkError(new Error('NETWORK unreachable'))).toBe(true);
    expect(isNetworkError(new Error('FETCH aborted'))).toBe(true);
    expect(isNetworkError(new Error('TIMEOUT exceeded'))).toBe(true);
    expect(isNetworkError(new Error('CONNECTION reset'))).toBe(true);
  });

  it('matches a keyword appearing anywhere inside a longer message', () => {
    expect(isNetworkError(new Error('TypeError: Failed to fetch'))).toBe(true);
  });
});

describe('isNetworkError — the instanceof Error guard is load-bearing', () => {
  it('returns false for a plain object whose .message matches the regex but is not an Error', () => {
    expect(isNetworkError({ message: 'network error' })).toBe(false);
  });

  it('returns true for an Error subclass carrying a matching message', () => {
    class HttpError extends Error {}
    expect(isNetworkError(new HttpError('connection reset by peer'))).toBe(true);
  });
});

describe('isNetworkError — adversarial catch-block payloads', () => {
  it.each<unknown>(['network', null, undefined, 42, { message: 'timeout' }, ['network']])(
    '%p is not an Error, so it is never classified as a network error',
    (value) => {
      expect(isNetworkError(value)).toBe(false);
    },
  );

  it('an Error with an empty message does not match', () => {
    expect(isNetworkError(new Error(''))).toBe(false);
  });
});

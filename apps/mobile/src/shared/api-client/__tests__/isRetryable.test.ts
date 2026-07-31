import fc from 'fast-check';

import { ApiError, NetworkError, isAbort, isRetryable, isSessionFetchFailure } from '../errors';

describe('isRetryable — consumed by the QueryClient predicate (retry: (n, error) => isRetryable(error) && n < 5)', () => {
  it('ApiError carries the "ApiError" discriminator alongside its status, the same way NetworkError carries "NetworkError"', () => {
    const error = new ApiError(500, 'x');
    expect(error).toMatchObject({ name: 'ApiError', status: 500 });
  });

  describe('ApiError status ladder', () => {
    it.each<[number, boolean]>([
      [400, false],
      [428, false],
      [429, true],
      [430, false],
      [499, false],
      [500, true],
      [501, true],
      [503, true],
    ])('status %i -> retryable: %s', (status, expected) => {
      expect(isRetryable(new ApiError(status, 'x'))).toBe(expected);
    });
  });

  describe('NetworkError', () => {
    it.each<'timeout' | 'transport'>(['timeout', 'transport'])(
      'a NetworkError with failure "%s" is retryable',
      (failure) => {
        expect(isRetryable(new NetworkError(failure, 'x'))).toBe(true);
      },
    );
  });

  it('a bare Error (neither ApiError nor NetworkError) is not retryable', () => {
    expect(isRetryable(new Error('boom'))).toBe(false);
  });

  it.each([null, undefined, 'a string', 42, {}])('%p is not retryable', (value) => {
    expect(isRetryable(value)).toBe(false);
  });

  describe('law: an ApiError is retryable iff its status is 429 or at least 500', () => {
    it('holds for any generated integer status', () => {
      fc.assert(
        fc.property(fc.integer(), (status) => {
          const expected = status === 429 || status >= 500;
          expect(isRetryable(new ApiError(status, 'x'))).toBe(expected);
        }),
      );
    });
  });

  describe('law: a NetworkError is retryable regardless of which failure kind it carries', () => {
    it('holds for both failure kinds', () => {
      fc.assert(
        fc.property(fc.constantFrom<'timeout' | 'transport'>('timeout', 'transport'), (failure) => {
          expect(isRetryable(new NetworkError(failure, 'x'))).toBe(true);
        }),
      );
    });
  });
});

describe('isAbort', () => {
  it.each<[unknown, boolean]>([
    [null, false],
    [undefined, false],
    ['AbortError', false],
    [42, false],
    [{}, false],
    [{ name: 'Error' }, false],
    [{ name: 'abortError' }, false],
    [{ name: 'AbortError' }, true],
  ])('%p -> %s', (value, expected) => {
    expect(isAbort(value)).toBe(expected);
  });
});

describe('isSessionFetchFailure', () => {
  it.each<[unknown, boolean]>([
    [null, false],
    [undefined, false],
    ['AuthRetryableFetchError', false],
    [42, false],
    [{}, false],
    [{ name: 'AuthApiError' }, false],
    [{ name: 'AuthRetryableFetchError' }, true],
  ])('%p -> %s', (value, expected) => {
    expect(isSessionFetchFailure(value)).toBe(expected);
  });
});

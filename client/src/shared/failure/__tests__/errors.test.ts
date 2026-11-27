import fc from 'fast-check';
import {
  ApiError,
  NetworkError,
  isAbort,
  isRetryable,
  isSessionFetchFailure,
  isNetworkError,
  kindOfStatus,
} from '@shared/failure';

const SERVER_FAULT = 500;

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

  describe('law: an ApiError is retryable iff its status is rate-limited or a server fault', () => {
    it('holds for any generated integer status', () => {
      fc.assert(
        fc.property(fc.integer(), (status) => {
          const kind = kindOfStatus(status);
          const expected = kind === 'rate-limited' || kind === 'server';
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

describe('isNetworkError — classified by the type of the throw, not by its message', () => {
  it.each(['timeout', 'transport'] as const)(
    'a NetworkError of kind %s is a network error',
    (kind) => {
      expect(isNetworkError(new NetworkError(kind, 'the API is unreachable'))).toBe(true);
    },
  );

  it('a session-read fetch failure from the auth SDK is a network error', () => {
    expect(isNetworkError({ name: 'AuthRetryableFetchError', message: 'anything' })).toBe(true);
  });

  it.each(['Network request failed', 'Failed to fetch', 'Load failed'])(
    'the TypeError a runtime fetch throws for "%s" is a network error',
    (message) => {
      expect(isNetworkError(new TypeError(message))).toBe(true);
    },
  );
});

describe('isNetworkError — message text alone never makes a network error', () => {
  it.each([
    'a network error',
    'fetch failed',
    'request timeout',
    'connection refused',
    'Network request failed',
  ])('a plain Error saying "%s" is not one', (message) => {
    expect(isNetworkError(new Error(message))).toBe(false);
  });

  it('a TypeError that is a programming error is not one, even if it mentions fetch', () => {
    expect(isNetworkError(new TypeError('fetch is not a function'))).toBe(false);
    expect(
      isNetworkError(new TypeError("Cannot read properties of undefined (reading 'network')")),
    ).toBe(false);
  });

  it('an ApiError is a server response, never transport, whatever its path says', () => {
    expect(
      isNetworkError(new ApiError(SERVER_FAULT, 'API /v1/discovery/search?q=network returned 500')),
    ).toBe(false);
  });

  it('a message that changes after construction does not change the answer', () => {
    const err = new Error('boom');
    err.message = 'Failed to fetch';
    expect(isNetworkError(err)).toBe(false);
  });
});

describe('isNetworkError — adversarial catch-block payloads', () => {
  it.each<unknown>(['network', null, undefined, 42, { message: 'timeout' }, ['network']])(
    '%p is not a network error',
    (value) => {
      expect(isNetworkError(value)).toBe(false);
    },
  );
});

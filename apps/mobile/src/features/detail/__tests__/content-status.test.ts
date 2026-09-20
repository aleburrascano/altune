import { ApiError, NetworkError } from '@shared/errors';

import {
  contentFailure,
  failFastOnSettledContentFailure,
  isContentError,
  isSettledContentFailure,
} from '../content-status';

describe('isSettledContentFailure', () => {
  it.each([
    [404, 'discovery.content_unserved'],
    [504, 'discovery.provider_timeout'],
    [503, 'discovery.provider_rate_limited'],
    [503, 'discovery.provider_circuit_open'],
    [502, 'discovery.provider_error'],
  ])('is settled for %i %s', (status, code) => {
    expect(isSettledContentFailure(new ApiError(status, 'x', code))).toBe(true);
  });

  it.each([
    ['a 5xx with an unrelated code', new ApiError(503, 'x', 'internal')],
    ['a 5xx without a code', new ApiError(503, 'x')],
    ['a network failure', new NetworkError('timeout', 'x')],
    ['a non-error value', 'boom'],
  ])('is not settled for %s', (_label, error) => {
    expect(isSettledContentFailure(error)).toBe(false);
  });
});

describe('failFastOnSettledContentFailure', () => {
  const settled = new ApiError(503, 'x', 'discovery.provider_circuit_open');
  const other = new ApiError(503, 'x', 'internal');

  it('never retries a settled content failure, whatever the fallback', () => {
    expect(failFastOnSettledContentFailure(true)(0, settled)).toBe(false);
    expect(failFastOnSettledContentFailure(() => true)(0, settled)).toBe(false);
  });

  it('defers to a function fallback for other failures', () => {
    const fallback = jest.fn((count: number) => count < 2);

    expect(failFastOnSettledContentFailure(fallback)(1, other)).toBe(true);
    expect(failFastOnSettledContentFailure(fallback)(2, other)).toBe(false);
    expect(fallback).toHaveBeenCalledWith(1, other);
  });

  it('reads boolean, number and unset fallbacks the way react-query does', () => {
    expect(failFastOnSettledContentFailure(false)(0, other)).toBe(false);
    expect(failFastOnSettledContentFailure(true)(99, other)).toBe(true);
    expect(failFastOnSettledContentFailure(1)(0, other)).toBe(true);
    expect(failFastOnSettledContentFailure(1)(1, other)).toBe(false);
    expect(failFastOnSettledContentFailure(undefined)(2, other)).toBe(true);
    expect(failFastOnSettledContentFailure(undefined)(3, other)).toBe(false);
  });
});

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

describe('contentFailure', () => {
  it.each([
    ['unserved content', new ApiError(404, 'x', 'discovery.content_unserved')],
    ['a settled provider failure', new ApiError(503, 'x', 'discovery.provider_circuit_open')],
  ])('is settled for %s', (_label, error) => {
    expect(contentFailure(true, error, undefined)).toBe('settled');
  });

  it.each([
    ['a dropped connection', new NetworkError('transport', 'x')],
    ['a 5xx without a discovery code', new ApiError(503, 'x', 'internal')],
  ])('is transient for %s', (_label, error) => {
    expect(contentFailure(true, error, undefined)).toBe('transient');
  });

  it.each(['timeout', 'rate_limited', 'circuit_open', 'error'] as const)(
    'is transient for a %s provider status on a successful response',
    (status) => {
      expect(contentFailure(false, null, { status })).toBe('transient');
    },
  );

  it('is null when nothing failed', () => {
    expect(contentFailure(false, null, { status: 'ok' })).toBeNull();
    expect(contentFailure(false, null, undefined)).toBeNull();
  });
});

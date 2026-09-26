import { isNetworkError } from '../isNetworkError';
import { apiFetch } from '@shared/api-client';
import { NetworkError } from '@shared/errors';
import { supabase } from '@shared/auth/supabaseClient';
import { describeError } from '../describeError';

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn() } },
}));

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
    expect(isNetworkError(new Error(message))).toBe(expected);
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

describe('the NetworkError the api-client throws', () => {
  const { __http } = require('../../../../jest/doubles/fetch.js');

  const getSession = supabase.auth.getSession as jest.Mock;

  beforeEach(() => {
    getSession.mockReset();
    getSession.mockResolvedValue({ data: { session: { access_token: 'tok' } }, error: null });
  });

  afterEach(() => {
    jest.useRealTimers();
  });

  async function settle(promise: Promise<unknown>): Promise<unknown> {
    return promise.then(
      () => {
        throw new Error('expected apiFetch to reject');
      },
      (e: unknown) => e,
    );
  }

  describe('isNetworkError — the NetworkError the api-client actually throws', () => {
    it('recognizes a NetworkError(transport) from an unreachable server, whose message names no keyword', async () => {
      __http.fail('GET /v1/library');

      const error = await settle(apiFetch('/v1/library'));

      expect(error).toBeInstanceOf(NetworkError);
      expect((error as Error).message).toBe('API http://127.0.0.1:8000/v1/library is unreachable');
      expect(isNetworkError(error)).toBe(true);
      expect(describeError(error).title).toBe('No connection');
    });

    it('recognizes a NetworkError(timeout) from a request past the deadline ("timed out", not "timeout")', async () => {
      jest.useFakeTimers();
      __http.hang('GET /v1/library');

      const caught = settle(apiFetch('/v1/library'));
      await Promise.resolve();
      await Promise.resolve();
      await Promise.resolve();
      jest.advanceTimersByTime(15_000);
      const error = await caught;

      expect(error).toMatchObject({ name: 'NetworkError', failure: 'timeout' });
      expect((error as Error).message).toMatch(/timed out after 15000ms$/);
      expect(isNetworkError(error)).toBe(true);
    });

    it('recognizes a NetworkError(transport) when the session refresh cannot reach the auth server', async () => {
      const offline = new Error('Failed');
      offline.name = 'AuthRetryableFetchError';
      getSession.mockResolvedValue({ data: { session: null }, error: offline });

      const error = await settle(apiFetch('/v1/library'));

      expect((error as Error).message).toBe('API /v1/library could not reach the auth server');
      expect(isNetworkError(error)).toBe(true);
    });

    it.each([
      new NetworkError('transport', ''),
      new NetworkError('timeout', 'slow'),
      new NetworkError('transport', 'truncated response'),
    ])('classifies %p by its name, whatever its message says', (error) => {
      expect(isNetworkError(error)).toBe(true);
    });

    it('does not classify a non-Error object merely named NetworkError', () => {
      expect(isNetworkError({ name: 'NetworkError', message: 'x' })).toBe(false);
    });

    it('does not classify an Error with a different name and no transport wording', () => {
      const error = new Error('API /v1/library returned 500');
      error.name = 'ApiError';
      expect(isNetworkError(error)).toBe(false);
    });
  });
});

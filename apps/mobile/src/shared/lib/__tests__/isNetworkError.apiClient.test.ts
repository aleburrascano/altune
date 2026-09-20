import { apiFetch } from '@shared/api-client';
import { NetworkError } from '@shared/errors';
import { supabase } from '@shared/auth/supabaseClient';

import { describeError } from '../describeError';
import { isNetworkError } from '../isNetworkError';

const { __http } = require('../../../../jest/doubles/fetch.js');

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn() } },
}));

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

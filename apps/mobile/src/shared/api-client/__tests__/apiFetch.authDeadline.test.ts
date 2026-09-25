import { apiFetch, NetworkError } from '../index';
import { audioRequestHeaders } from '../audio';
import { supabase } from '@shared/auth/supabaseClient';

const { __http } = require('../../../../jest/doubles/fetch.js');

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn() } },
}));

const getSession = supabase.auth.getSession as jest.Mock;

let warn: jest.SpyInstance;

beforeEach(() => {
  jest.useFakeTimers();
  getSession.mockReset();
  getSession.mockReturnValue(new Promise(() => undefined));
  warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
});

afterEach(() => {
  warn.mockRestore();
  jest.useRealTimers();
});

async function flushMicrotasks(): Promise<void> {
  for (let i = 0; i < 5; i += 1) await Promise.resolve();
}

describe('apiFetch() when the token lookup never settles', () => {
  it('is still pending at 14999ms and rejects with NetworkError(timeout) at 15000ms without sending', async () => {
    let settled = false;
    const caught = apiFetch('/v1/library/tracks').catch((error: unknown) => {
      settled = true;
      return error;
    });

    jest.advanceTimersByTime(14_999);
    await flushMicrotasks();
    expect(settled).toBe(false);

    jest.advanceTimersByTime(1);
    const error = await caught;

    expect(error).toBeInstanceOf(NetworkError);
    expect(error).toMatchObject({ failure: 'timeout' });
    expect(__http.requests).toHaveLength(0);
    expect(jest.getTimerCount()).toBe(0);
  });

  it('logs the timeout as a redacted failure line', async () => {
    const caught = apiFetch('/v1/library/tracks?q=secret').catch(() => undefined);

    jest.advanceTimersByTime(15_000);
    await caught;

    expect(warn).toHaveBeenCalledWith(
      '[api] request failed',
      expect.objectContaining({ path: '/v1/library/tracks', failure: 'timeout' }),
    );
  });
});

describe('audioRequestHeaders() when the token lookup never settles', () => {
  it('resolves without an Authorization header once the lookup deadline passes', async () => {
    const pending = audioRequestHeaders();

    jest.advanceTimersByTime(15_000);
    const headers = await pending;

    expect(headers).not.toHaveProperty('Authorization');
  });
});

import { AUTH_FETCH_TIMEOUT_MS, fetchWithinAuthDeadline } from '../authDeadline';

const { __http } = require('../../../../jest/doubles/fetch.js');

const AUTH_TOKEN_URL = 'https://fixture.supabase.co/auth/v1/token';

beforeEach(() => {
  jest.useFakeTimers();
});

afterEach(() => {
  jest.useRealTimers();
});

describe('fetchWithinAuthDeadline(), the fetch the Supabase client is built with', () => {
  it('aborts a request the auth server never answers once the fetch deadline passes', async () => {
    __http.hang('POST /auth/v1/token');
    const caught = fetchWithinAuthDeadline(AUTH_TOKEN_URL, { method: 'POST' }).catch(
      (error: unknown) => error,
    );

    jest.advanceTimersByTime(AUTH_FETCH_TIMEOUT_MS);

    expect(await caught).toMatchObject({ name: 'AbortError' });
    expect(jest.getTimerCount()).toBe(0);
  });

  it('relays an abort from the caller signal', async () => {
    __http.hang('POST /auth/v1/token');
    const controller = new AbortController();
    const caught = fetchWithinAuthDeadline(AUTH_TOKEN_URL, {
      method: 'POST',
      signal: controller.signal,
    }).catch((error: unknown) => error);

    controller.abort();

    expect(await caught).toMatchObject({ name: 'AbortError' });
    expect(jest.getTimerCount()).toBe(0);
  });

  it('aborts at once when the caller signal is already aborted', async () => {
    __http.hang('POST /auth/v1/token');
    const controller = new AbortController();
    controller.abort();
    const caught = fetchWithinAuthDeadline(AUTH_TOKEN_URL, {
      method: 'POST',
      signal: controller.signal,
    }).catch((error: unknown) => error);

    expect(await caught).toMatchObject({ name: 'AbortError' });
    expect(jest.getTimerCount()).toBe(0);
  });

  it('returns the response and leaves no timer behind when the server answers', async () => {
    __http.reply('POST /auth/v1/token', { status: 200, json: { access_token: 'tok' } });

    const response = await fetchWithinAuthDeadline(AUTH_TOKEN_URL, { method: 'POST' });

    expect(response.status).toBe(200);
    expect(jest.getTimerCount()).toBe(0);
  });
});

describe('fetchWithinAuthDeadline() body read', () => {
  function stalledBodyResponse(signal: AbortSignal): unknown {
    const stall = (): Promise<never> =>
      new Promise((_resolve, reject) => {
        signal.addEventListener('abort', () => {
          const error = new Error('The operation was aborted');
          error.name = 'AbortError';
          reject(error);
        });
      });
    const response = { status: 200, body: {}, arrayBuffer: stall, clone: () => response };
    return response;
  }

  it('rejects the body read once the deadline passes after headers arrived', async () => {
    const fetchSpy = jest
      .spyOn(globalThis, 'fetch')
      .mockImplementation((_input, init) =>
        Promise.resolve(stalledBodyResponse(init?.signal as AbortSignal) as Response),
      );
    const response = await fetchWithinAuthDeadline(AUTH_TOKEN_URL, { method: 'POST' });
    const caught = response.arrayBuffer().catch((error: unknown) => error);

    jest.advanceTimersByTime(AUTH_FETCH_TIMEOUT_MS);

    expect(await caught).toMatchObject({ name: 'AbortError' });
    expect(jest.getTimerCount()).toBe(0);
    fetchSpy.mockRestore();
  });

  it('keeps the status, headers and body of a fast response readable', async () => {
    const real = new Response('{"access_token":"tok"}', {
      status: 201,
      headers: { 'x-trace': 'abc' },
    });
    const fetchSpy = jest.spyOn(globalThis, 'fetch').mockResolvedValue(real);

    const response = await fetchWithinAuthDeadline(AUTH_TOKEN_URL, { method: 'POST' });
    const body = await response.text();
    await Promise.resolve();
    await Promise.resolve();

    expect(response.status).toBe(201);
    expect(response.headers.get('x-trace')).toBe('abc');
    expect(body).toBe('{"access_token":"tok"}');
    expect(jest.getTimerCount()).toBe(0);
    fetchSpy.mockRestore();
  });
});

describe('supabase client construction', () => {
  it('hands the auth SDK the bounded fetch', () => {
    jest.isolateModules(() => {
      const createClient = jest.fn(() => ({}));
      jest.doMock('@supabase/supabase-js', () => ({ createClient }));
      require('../supabaseClient');
      const options = (createClient.mock.calls[0] as unknown[])[2] as {
        global?: { fetch?: unknown };
      };
      expect(options.global?.fetch).toBe(require('../authDeadline').fetchWithinAuthDeadline);
    });
  });
});

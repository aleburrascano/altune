import { fetchWithinAuthDeadline } from '@/mobile';

const AUTH_FETCH_TIMEOUT_MS = 10_000;

const { fakeFetch, __http } = require('@testing/doubles/fetch.js');

const AUTH_TOKEN_URL = 'https://fixture.supabase.co/auth/v1/token';

const boundedFakeFetch = fetchWithinAuthDeadline(fakeFetch);

beforeEach(() => {
  jest.useFakeTimers();
});

afterEach(() => {
  jest.useRealTimers();
});

describe('fetchWithinAuthDeadline(), the fetch the Supabase client is built with', () => {
  it('aborts a request the auth server never answers once the fetch deadline passes', async () => {
    __http.hang('POST /auth/v1/token');
    const caught = boundedFakeFetch(AUTH_TOKEN_URL, { method: 'POST' }).catch(
      (error: unknown) => error,
    );

    jest.advanceTimersByTime(AUTH_FETCH_TIMEOUT_MS);

    expect(await caught).toMatchObject({ name: 'AbortError' });
    expect(jest.getTimerCount()).toBe(0);
  });

  it('relays an abort from the caller signal', async () => {
    __http.hang('POST /auth/v1/token');
    const controller = new AbortController();
    const caught = boundedFakeFetch(AUTH_TOKEN_URL, {
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
    const caught = boundedFakeFetch(AUTH_TOKEN_URL, {
      method: 'POST',
      signal: controller.signal,
    }).catch((error: unknown) => error);

    expect(await caught).toMatchObject({ name: 'AbortError' });
    expect(jest.getTimerCount()).toBe(0);
  });

  it('returns the response and leaves no timer behind when the server answers', async () => {
    __http.reply('POST /auth/v1/token', { status: 200, json: { access_token: 'tok' } });

    const response = await boundedFakeFetch(AUTH_TOKEN_URL, { method: 'POST' });

    expect(response.status).toBe(200);
    expect(jest.getTimerCount()).toBe(0);
  });

  it('releases the deadline when the request itself fails', async () => {
    const failure = new TypeError('Network request failed');
    const failing = fetchWithinAuthDeadline(() => Promise.reject(failure));

    await expect(failing(AUTH_TOKEN_URL, { method: 'POST' })).rejects.toBe(failure);
    expect(jest.getTimerCount()).toBe(0);
  });
});

describe('fetchWithinAuthDeadline() body read', () => {
  function stalledBodyResponse(signal: AbortSignal): Response {
    const stall = (): Promise<never> =>
      new Promise((_resolve, reject) => {
        signal.addEventListener('abort', () => {
          const error = new Error('The operation was aborted');
          error.name = 'AbortError';
          reject(error);
        });
      });
    const response: Partial<Response> = {
      status: 200,
      body: {} as ReadableStream,
      arrayBuffer: stall,
      clone: () => response as Response,
    };
    return response as Response;
  }

  it('rejects the body read once the deadline passes after headers arrived', async () => {
    const stalling = fetchWithinAuthDeadline((_input, init) =>
      Promise.resolve(stalledBodyResponse(init?.signal as AbortSignal)),
    );
    const response = await stalling(AUTH_TOKEN_URL, { method: 'POST' });
    const caught = response.arrayBuffer().catch((error: unknown) => error);

    jest.advanceTimersByTime(AUTH_FETCH_TIMEOUT_MS);

    expect(await caught).toMatchObject({ name: 'AbortError' });
    expect(jest.getTimerCount()).toBe(0);
  });

  it('keeps the status, headers and body of a fast response readable', async () => {
    const real = new Response('{"access_token":"tok"}', {
      status: 201,
      headers: { 'x-trace': 'abc' },
    });
    const answering = fetchWithinAuthDeadline(() => Promise.resolve(real));

    const response = await answering(AUTH_TOKEN_URL, { method: 'POST' });
    const body = await response.text();
    await Promise.resolve();
    await Promise.resolve();

    expect(response.status).toBe(201);
    expect(response.headers.get('x-trace')).toBe('abc');
    expect(body).toBe('{"access_token":"tok"}');
    expect(jest.getTimerCount()).toBe(0);
  });
});

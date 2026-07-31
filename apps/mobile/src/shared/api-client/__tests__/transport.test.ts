import { apiFetch } from '../index';
import { NetworkError } from '../errors';
import { supabase } from '@shared/auth/supabaseClient';

const { __http } = require('../../../../jest/doubles/fetch.js');

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn() } },
}));

const getSession = supabase.auth.getSession as jest.Mock;

function withSession(accessToken = 'tok') {
  getSession.mockResolvedValue({
    data: { session: { access_token: accessToken } },
    error: null,
  });
}

beforeEach(() => {
  getSession.mockReset();
  withSession();
});

afterEach(() => {
  jest.useRealTimers();
});

describe('send(): a caller-supplied AbortController firing (component unmount mid-searchDiscovery)', () => {
  it('rethrows the caller abort untouched, not as a NetworkError', async () => {
    __http.hang('GET /v1/discovery/search');
    const controller = new AbortController();

    const pending = apiFetch('/v1/discovery/search', { signal: controller.signal });
    const caught = pending.catch((e: unknown) => e);
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();

    controller.abort();

    const error = await caught;
    expect(error).not.toBeInstanceOf(NetworkError);
    expect((error as Error).name).toBe('AbortError');
  });

  it('rethrows immediately when the caller signal was already aborted before the request even started (a stale page cancelled by type-ahead debounce)', async () => {
    __http.reply('GET /v1/discovery/search', { status: 200, json: {} });
    const controller = new AbortController();
    controller.abort();

    const error = await apiFetch('/v1/discovery/search', { signal: controller.signal }).catch(
      (e: unknown) => e,
    );

    expect(error).not.toBeInstanceOf(NetworkError);
    expect((error as Error).name).toBe('AbortError');
  });

  it('has no effect when the caller aborts after the request already completed and release() ran', async () => {
    __http.reply('GET /v1/discovery/search', { status: 200, json: {} });
    const controller = new AbortController();

    await apiFetch('/v1/discovery/search', { signal: controller.signal });

    expect(() => controller.abort()).not.toThrow();
  });
});

describe('send(): cancelled() is checked before expired() — the order is load-bearing', () => {
  it('reports the caller cancellation, not a timeout, even when the internal deadline timer also fires in the same tick', async () => {
    jest.useFakeTimers();
    __http.hang('GET /v1/discovery/search');
    const controller = new AbortController();

    const pending = apiFetch('/v1/discovery/search', { signal: controller.signal });
    const caught = pending.catch((e: unknown) => e);
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();

    jest.advanceTimersByTime(14_999);
    controller.abort();
    jest.advanceTimersByTime(1);

    const error = await caught;
    expect(error).not.toMatchObject({ name: 'NetworkError', failure: 'timeout' });
    expect((error as Error).name).toBe('AbortError');
  });
});

describe('send(): deadline.expired() — no response within REQUEST_TIMEOUT_MS (15000ms)', () => {
  it('is still in flight at 14999ms, and only becomes NetworkError(timeout) at exactly 15000ms', async () => {
    jest.useFakeTimers();
    __http.hang('GET /v1/discovery/search');

    const pending = apiFetch('/v1/discovery/search');
    const caught = pending.catch((e: Error) => e);
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();

    jest.advanceTimersByTime(14_999);
    let settled = false;
    caught.then(() => (settled = true));
    await Promise.resolve();
    await Promise.resolve();
    expect(settled).toBe(false);

    jest.advanceTimersByTime(1);
    await expect(caught).resolves.toMatchObject({ name: 'NetworkError', failure: 'timeout' });
  });
});

describe('send(): isAbort(cause) — an abort surfaced by the fetch implementation itself (e.g. iOS suspends the app and URLSession cancels the task)', () => {
  it('rethrows that residual AbortError untouched, when neither the deadline nor the caller triggered it', async () => {
    const residual = new Error('The operation was aborted');
    residual.name = 'AbortError';
    __http.fail('GET /v1/discovery/search', residual);

    await expect(apiFetch('/v1/discovery/search')).rejects.toBe(residual);
  });
});

describe('send(): a bare transport failure (DNS failure, airplane mode, connection refused)', () => {
  it('classifies it as NetworkError(transport)', async () => {
    __http.fail('GET /v1/discovery/search');

    await expect(apiFetch('/v1/discovery/search')).rejects.toMatchObject({
      name: 'NetworkError',
      failure: 'transport',
    });
  });
});

describe('deadline.release() runs on every send() failure branch, leaking no timer', () => {
  it('after a caller cancellation', async () => {
    jest.useFakeTimers();
    __http.hang('GET /v1/discovery/search');
    const controller = new AbortController();

    const caught = apiFetch('/v1/discovery/search', { signal: controller.signal }).catch(
      (e: unknown) => e,
    );
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
    controller.abort();
    await caught;

    expect(jest.getTimerCount()).toBe(0);
  });

  it('after a deadline timeout', async () => {
    jest.useFakeTimers();
    __http.hang('GET /v1/discovery/search');

    const caught = apiFetch('/v1/discovery/search').catch((e: unknown) => e);
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
    jest.advanceTimersByTime(15_000);
    await caught;

    expect(jest.getTimerCount()).toBe(0);
  });

  it('after a residual AbortError from the fetch implementation', async () => {
    jest.useFakeTimers();
    const residual = new Error('aborted');
    residual.name = 'AbortError';
    __http.fail('GET /v1/discovery/search', residual);

    await apiFetch('/v1/discovery/search').catch(() => {});

    expect(jest.getTimerCount()).toBe(0);
  });

  it('after a bare transport failure', async () => {
    jest.useFakeTimers();
    __http.fail('GET /v1/discovery/search');

    await apiFetch('/v1/discovery/search').catch(() => {});

    expect(jest.getTimerCount()).toBe(0);
  });
});

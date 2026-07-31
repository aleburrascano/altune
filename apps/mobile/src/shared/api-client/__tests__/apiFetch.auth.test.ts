import { apiFetch, apiBase, ApiError, NetworkError } from '../index';
import { supabase } from '@shared/auth/supabaseClient';
import { clearSessionExpired, getSessionExpired } from '@shared/auth/sessionExpired';

const { __http } = require('../../../../jest/doubles/fetch.js');

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn() } },
}));

const getSession = supabase.auth.getSession as jest.Mock;

function withSession(accessToken: string | null | undefined = 'tok') {
  getSession.mockResolvedValue({
    data: { session: accessToken == null ? null : { access_token: accessToken } },
    error: null,
  });
}

beforeEach(() => {
  clearSessionExpired();
  getSession.mockReset();
});

describe('authorization()', () => {
  it('throws NetworkError(transport) when the session refresh fails offline (AuthRetryableFetchError)', async () => {
    getSession.mockResolvedValue({
      data: { session: null },
      error: { name: 'AuthRetryableFetchError', message: 'network request failed' },
    });

    await expect(apiFetch('/v1/library/tracks')).rejects.toBeInstanceOf(NetworkError);
    expect(__http.requests.length).toBe(0);
  });

  it('throws ApiError(401) using the Supabase error message when the refresh token is stale/malformed', async () => {
    getSession.mockResolvedValue({
      data: { session: null },
      error: { name: 'AuthApiError', message: 'refresh_token_not_found' },
    });

    await expect(apiFetch('/v1/library/tracks')).rejects.toMatchObject({
      status: 401,
      message: expect.stringContaining('refresh_token_not_found'),
    });
    expect(__http.requests.length).toBe(0);
  });

  it('throws ApiError(401) with "no active session" when signed out with no error (first launch)', async () => {
    getSession.mockResolvedValue({ data: { session: null }, error: null });

    await expect(apiFetch('/v1/library/tracks')).rejects.toMatchObject({
      status: 401,
      message: expect.stringContaining('no active session'),
    });
    expect(__http.requests.length).toBe(0);
  });

  it('throws ApiError(401) when the session object carries no access_token', async () => {
    getSession.mockResolvedValue({ data: { session: {} }, error: null });

    await expect(apiFetch('/v1/library/tracks')).rejects.toBeInstanceOf(ApiError);
    expect(__http.requests.length).toBe(0);
  });

  it('propagates a rejected getSession() instead of silently succeeding', async () => {
    getSession.mockRejectedValue(new Error('secure store unavailable'));

    await expect(apiFetch('/v1/library/tracks')).rejects.toThrow('secure store unavailable');
    expect(__http.requests.length).toBe(0);
  });

  it('never sends a request when there is no usable session', async () => {
    getSession.mockResolvedValue({ data: { session: null }, error: null });

    await expect(apiFetch('/v1/library/tracks')).rejects.toBeInstanceOf(ApiError);
    expect(() => __http.last()).not.toThrow();
    expect(__http.requests.length).toBe(0);
  });

  it('fails closed with ApiError(401) when error is set even though a session with an access_token is also present', async () => {
    getSession.mockResolvedValue({
      data: { session: { access_token: 'still-cached-token' } },
      error: { name: 'AuthApiError', message: 'signed out on another device' },
    });

    await expect(apiFetch('/v1/library/tracks')).rejects.toMatchObject({ status: 401 });
    expect(__http.requests.length).toBe(0);
  });
});

describe('apiFetch header merge', () => {
  it('sends ngrok-skip-browser-warning, the bearer token, then merges caller headers, with the token only in Authorization', async () => {
    withSession('secret-token-value');
    __http.reply('GET /v1/library/tracks', { status: 200, json: [] });

    await apiFetch('/v1/library/tracks', { headers: { 'Content-Type': 'application/json' } });

    const request = __http.last();
    expect(request.headers.Authorization).toBe('Bearer secret-token-value');
    expect(request.headers['ngrok-skip-browser-warning']).toBe('1');
    expect(request.headers['Content-Type']).toBe('application/json');
    expect(request.url).not.toContain('secret-token-value');
    expect(request.query).not.toContain('secret-token-value');
  });

  it('lets caller headers override the computed defaults', async () => {
    withSession();
    __http.reply('POST /v1/feedback/reports', { status: 202 });

    await apiFetch('/v1/feedback/reports', {
      method: 'POST',
      headers: { 'ngrok-skip-browser-warning': '0' },
    });

    expect(__http.last().headers['ngrok-skip-browser-warning']).toBe('0');
  });

  it('FOOTGUN: a caller-supplied Authorization header silently wins over the wrapper-injected bearer token, because the caller spread is last', async () => {
    withSession('wrapper-computed-token');
    __http.reply('GET /v1/library/tracks', { status: 200, json: [] });

    await apiFetch('/v1/library/tracks', {
      headers: { Authorization: 'Bearer caller-supplied-token' },
    });

    expect(__http.last().headers.Authorization).toBe('Bearer caller-supplied-token');
  });
});

describe('server-side 401 marks the session expired without signing out', () => {
  it('calls markSessionExpired on a server 401 (revoked/rotated token the SDK still trusted)', async () => {
    withSession();
    __http.reply('GET /v1/library/tracks', { status: 401, json: { message: 'revoked' } });

    await expect(apiFetch('/v1/library/tracks')).rejects.toMatchObject({ status: 401 });
    expect(getSessionExpired()).toBe(true);
  });

  it('does not mark the session expired on a 500 (backend fault, not an auth problem)', async () => {
    withSession();
    __http.reply('GET /v1/library/tracks', { status: 500, json: { message: 'boom' } });

    await expect(apiFetch('/v1/library/tracks')).rejects.toMatchObject({ status: 500 });
    expect(getSessionExpired()).toBe(false);
  });
});

describe('non-2xx status ladder throws ApiError(status)', () => {
  const cases: [number, string][] = [
    [400, 'PATCH /v1/feedback/reports'],
    [403, 'GET /v1/library/tracks'],
    [404, 'DELETE /v1/library/playlists/p1'],
    [409, 'PATCH /v1/library/playlists/p1'],
    [429, 'GET /v1/library/tracks'],
    [500, 'GET /v1/library/tracks'],
    [503, 'GET /v1/library/tracks'],
  ];

  it.each(cases)('status %i throws ApiError with that status', async (status, spec) => {
    withSession();
    __http.reply(spec, { status, json: { message: 'nope' } });
    const parts = spec.split(' ');
    expect(parts[0]).toBeDefined();
    expect(parts[1]).toBeDefined();
    const method = parts[0] as string;
    const path = parts[1] as string;

    await expect(apiFetch(path, { method })).rejects.toMatchObject({ status });
  });

  it('only a 401 among the ladder marks the session expired', async () => {
    withSession();
    __http.reply('GET /v1/library/tracks', { status: 403, json: {} });

    await expect(apiFetch('/v1/library/tracks')).rejects.toMatchObject({ status: 403 });
    expect(getSessionExpired()).toBe(false);
  });
});

describe('readBody status short-circuit (202/204 vs 200/201), and the unreachable 304 arm', () => {
  it.each([202, 204])('returns undefined for status %i without parsing a body', async (status) => {
    withSession();
    __http.reply('DELETE /v1/library/playlists/p1/tracks/t1', { status, malformed: true });

    await expect(
      apiFetch('/v1/library/playlists/p1/tracks/t1', { method: 'DELETE' }),
    ).resolves.toBeUndefined();
  });

  it('throws ApiError(304) and never parses a body, since 304 is outside response.ok', async () => {
    withSession();
    __http.reply('DELETE /v1/library/playlists/p1/tracks/t1', {
      status: 304,
      malformed: true,
    });

    await expect(
      apiFetch('/v1/library/playlists/p1/tracks/t1', { method: 'DELETE' }),
    ).rejects.toMatchObject({ status: 304 });
  });

  it('parses the body on 200', async () => {
    withSession();
    __http.reply('GET /v1/library/tracks', { status: 200, json: { id: 't1', title: 'A Track' } });

    await expect(apiFetch('/v1/library/tracks')).resolves.toEqual({ id: 't1', title: 'A Track' });
  });

  it('parses the body on 201', async () => {
    withSession();
    __http.reply('POST /v1/library/playlists', { status: 201, json: { id: 'p1' } });

    await expect(apiFetch('/v1/library/playlists', { method: 'POST' })).resolves.toEqual({
      id: 'p1',
    });
  });
});

describe('readBody adversarial payloads', () => {
  it('resolves a JSON null body as-is rather than throwing', async () => {
    withSession();
    __http.reply('GET /v1/library/tracks', { status: 200, json: null });

    await expect(apiFetch('/v1/library/tracks')).resolves.toBeNull();
  });

  it('resolves an array where a record was expected as-is (no shape validation)', async () => {
    withSession();
    __http.reply('GET /v1/library/tracks', { status: 200, json: [1, 2, 3] });

    await expect(apiFetch<{ id: string }>('/v1/library/tracks')).resolves.toEqual([1, 2, 3]);
  });

  it('throws NetworkError(transport) for a truncated/malformed body (e.g. an ngrok warning page)', async () => {
    withSession();
    __http.reply('GET /v1/library/tracks', { status: 200, malformed: true });

    await expect(apiFetch('/v1/library/tracks')).rejects.toBeInstanceOf(NetworkError);
    await expect(apiFetch('/v1/library/tracks')).rejects.toMatchObject({ failure: 'transport' });
  });
});

describe('security: the access token never leaks off the Authorization header', () => {
  it('does not appear in the request URL, the query string, or any thrown error message', async () => {
    withSession('leak-check-token');
    __http.reply('GET /v1/library/tracks', { status: 500, json: {} });

    let thrown: unknown;
    try {
      await apiFetch('/v1/library/tracks?q=hello');
    } catch (error) {
      thrown = error;
    }

    expect(__http.last().url).not.toContain('leak-check-token');
    expect(__http.last().query).not.toContain('leak-check-token');
    expect((thrown as Error).message).not.toContain('leak-check-token');
    expect((thrown as Error).stack ?? '').not.toContain('leak-check-token');
  });

  it('does not appear in the ApiError message thrown on a pre-flight 401 (stale refresh token)', async () => {
    getSession.mockResolvedValue({
      data: { session: null },
      error: { name: 'AuthApiError', message: 'Invalid Refresh Token: Already Used' },
    });

    await expect(apiFetch('/v1/library/tracks')).rejects.toMatchObject({
      status: 401,
      message: expect.not.stringContaining('leak-check-token'),
    });
    expect(__http.requests.length).toBe(0);
  });
});

describe('deadline release on every path', () => {
  it('does not leave a dangling timer after a successful request', async () => {
    jest.useFakeTimers();
    withSession();
    __http.reply('GET /v1/library/tracks', { status: 200, json: {} });

    await apiFetch('/v1/library/tracks');

    expect(jest.getTimerCount()).toBe(0);
    jest.useRealTimers();
  });

  it('does not leave a dangling timer after a thrown ApiError (non-2xx)', async () => {
    jest.useFakeTimers();
    withSession();
    __http.reply('GET /v1/library/tracks', { status: 404, json: {} });

    await expect(apiFetch('/v1/library/tracks')).rejects.toBeInstanceOf(ApiError);

    expect(jest.getTimerCount()).toBe(0);
    jest.useRealTimers();
  });

  it('does not leave a dangling timer after a thrown NetworkError (truncated body)', async () => {
    jest.useFakeTimers();
    withSession();
    __http.reply('GET /v1/library/tracks', { status: 200, malformed: true });

    await expect(apiFetch('/v1/library/tracks')).rejects.toBeInstanceOf(NetworkError);

    expect(jest.getTimerCount()).toBe(0);
    jest.useRealTimers();
  });

  it('does not leave a dangling timer when authorization() throws before any fetch', async () => {
    jest.useFakeTimers();
    getSession.mockResolvedValue({ data: { session: null }, error: null });

    await expect(apiFetch('/v1/library/tracks')).rejects.toBeInstanceOf(ApiError);

    expect(jest.getTimerCount()).toBe(0);
    jest.useRealTimers();
  });
});

describe('apiBase', () => {
  it('resolves to the documented loopback default when EXPO_PUBLIC_API_URL is unset', () => {
    expect(apiBase).toBe('http://127.0.0.1:8000');
  });

  it('is used to build the request URL', async () => {
    withSession();
    __http.reply('GET /v1/library/tracks', { status: 200, json: {} });

    await apiFetch('/v1/library/tracks');

    expect(__http.last().url).toBe(`${apiBase}/v1/library/tracks`);
    expect(__http.last().url.startsWith('http://127.0.0.1:8000')).toBe(true);
  });
});

import { Platform } from 'react-native';
import { apiFetch, apiBase, ApiError, NetworkError } from '../index';
import { supabase } from '@shared/auth/supabaseClient';
import {
  clearSessionExpired,
  getSessionExpired,
  renewSessionCredentials,
} from '@shared/auth/sessionExpired';
import { ContractError } from '@shared/errors';
import { runSignOutCleanups } from '@shared/session/signOutCleanup';

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

const originalOS = Platform.OS;

beforeEach(() => {
  clearSessionExpired();
  getSession.mockReset();
});

afterEach(() => {
  Platform.OS = originalOS;
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

  it('omits ngrok-skip-browser-warning on web, where the CORS allowlist would reject the preflight', async () => {
    Platform.OS = 'web';
    withSession();
    __http.reply('GET /v1/library/tracks', { status: 200, json: [] });

    await apiFetch('/v1/library/tracks');

    expect(__http.last().headers['ngrok-skip-browser-warning']).toBeUndefined();
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

  it('keeps the session bearer token when a caller supplies its own Authorization header', async () => {
    withSession('wrapper-computed-token');
    __http.reply('GET /v1/library/tracks', { status: 200, json: [] });

    await apiFetch('/v1/library/tracks', {
      headers: { Authorization: 'Bearer caller-supplied-token' },
    });

    expect(__http.last().headers.Authorization).toBe('Bearer wrapper-computed-token');
  });

  it('keeps the session bearer token when a caller forwards a whole header set that carries one', async () => {
    withSession('wrapper-computed-token');
    __http.reply('POST /v1/feedback/reports', { status: 202 });

    await apiFetch('/v1/feedback/reports', {
      method: 'POST',
      headers: {
        Authorization: 'Bearer forwarded-from-another-context',
        'Content-Type': 'application/json',
      },
    });

    expect(__http.last().headers.Authorization).toBe('Bearer wrapper-computed-token');
    expect(__http.last().headers['Content-Type']).toBe('application/json');
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

describe('the machine-readable error code (ADR-0021) surfaces on ApiError', () => {
  it('carries the error body code onto ApiError.code, and a caller can branch on it', async () => {
    withSession();
    __http.reply('GET /v1/tracks/t1', {
      status: 404,
      json: { detail: 'track not found', code: 'catalog.track_not_found' },
    });

    const error = await apiFetch('/v1/tracks/t1').catch((e: unknown) => e);

    expect(error).toBeInstanceOf(ApiError);
    expect((error as ApiError).code).toBe('catalog.track_not_found');

    const branch =
      error instanceof ApiError && error.code === 'catalog.track_not_found'
        ? 'missing-track'
        : 'other';
    expect(branch).toBe('missing-track');
  });

  it('leaves code undefined when the error body omits it, still yielding a usable ApiError', async () => {
    withSession();
    __http.reply('POST /v1/feedback/reports', { status: 400, json: { detail: 'message required' } });

    const error = await apiFetch('/v1/feedback/reports', { method: 'POST' }).catch(
      (e: unknown) => e,
    );

    expect(error).toBeInstanceOf(ApiError);
    expect((error as ApiError).status).toBe(400);
    expect((error as ApiError).code).toBeUndefined();
  });

  it('leaves code undefined when the error body is not JSON, degrading to status-only branching', async () => {
    withSession();
    __http.reply('GET /v1/library/tracks', { status: 503, malformed: true });

    const error = await apiFetch('/v1/library/tracks').catch((e: unknown) => e);

    expect(error).toBeInstanceOf(ApiError);
    expect((error as ApiError).status).toBe(503);
    expect((error as ApiError).code).toBeUndefined();
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

describe('token lookup deadline', () => {
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
});

describe('correlation id', () => {
  // Mirrors the Go API's accepted shape (httputil.CorrelationID): <=64 chars of [A-Za-z0-9_-].
  const SERVER_ACCEPTED_ID = /^[A-Za-z0-9_-]{1,64}$/;

  let warn: jest.SpyInstance;

  beforeEach(() => {
    getSession.mockReset();
    getSession.mockResolvedValue({ data: { session: { access_token: 'tok' } }, error: null });
    warn = jest.spyOn(console, 'warn').mockImplementation(() => {});
  });

  afterEach(() => {
    warn.mockRestore();
    Platform.OS = originalOS;
  });

  function sentCorrelationId(): string | undefined {
    return __http.last().headers['X-Correlation-ID'];
  }

  describe('apiFetch correlation id', () => {
    it('sends an X-Correlation-ID the server accepts on every outgoing request', async () => {
      __http.reply('GET /v1/playlists', { status: 200, json: { items: [] } });

      await apiFetch('/v1/playlists');
      const first = sentCorrelationId();
      await apiFetch('/v1/playlists');
      const second = sentCorrelationId();

      expect(first).toMatch(SERVER_ACCEPTED_ID);
      expect(second).toMatch(SERVER_ACCEPTED_ID);
      expect(second).not.toBe(first);
    });

    it('pairs a failed request with the id it sent, in the failure log line', async () => {
      __http.reply('GET /v1/playlists', { status: 503, json: { code: 'unavailable' } });

      await expect(apiFetch('/v1/playlists')).rejects.toMatchObject({ status: 503 });

      const sent = sentCorrelationId();
      expect(sent).toMatch(SERVER_ACCEPTED_ID);
      expect(warn).toHaveBeenCalledWith(
        '[api] request failed',
        expect.objectContaining({ correlationId: sent }),
      );
    });

    it('pairs a transport failure with the id it sent', async () => {
      __http.fail('GET /v1/playlists');

      await expect(apiFetch('/v1/playlists')).rejects.toMatchObject({ name: 'NetworkError' });

      expect(warn).toHaveBeenCalledWith(
        '[api] request failed',
        expect.objectContaining({ correlationId: sentCorrelationId() }),
      );
    });

    it('omits the header on web, where the API CORS policy would reject the preflight', async () => {
      Platform.OS = 'web';
      __http.reply('GET /v1/playlists', { status: 500, json: {} });

      await expect(apiFetch('/v1/playlists')).rejects.toMatchObject({ status: 500 });

      expect(sentCorrelationId()).toBeUndefined();
      expect(warn).toHaveBeenCalledWith(
        '[api] request failed',
        expect.not.objectContaining({ correlationId: expect.anything() }),
      );
    });
  });

  describe('apiFetch hands the correlation id to the caller on the error it throws', () => {
    it('a non-2xx response throws an ApiError carrying the id that request sent', async () => {
      __http.reply('GET /v1/playlists', { status: 503, json: { code: 'unavailable' } });

      const thrown = await apiFetch('/v1/playlists').catch((error: unknown) => error);

      expect(thrown).toMatchObject({ status: 503, correlationId: sentCorrelationId() });
    });

    it('an unreachable server throws a NetworkError carrying the id that request sent', async () => {
      __http.fail('GET /v1/playlists');

      const thrown = await apiFetch('/v1/playlists').catch((error: unknown) => error);

      expect(thrown).toMatchObject({ name: 'NetworkError', correlationId: sentCorrelationId() });
    });

    it('a truncated response body throws a NetworkError carrying the id that request sent', async () => {
      __http.reply('GET /v1/playlists', { status: 200, malformed: true });

      const thrown = await apiFetch('/v1/playlists').catch((error: unknown) => error);

      expect(thrown).toMatchObject({ name: 'NetworkError', correlationId: sentCorrelationId() });
    });

    it('a request refused before it leaves the device still carries the id it would have sent', async () => {
      getSession.mockResolvedValue({ data: { session: null }, error: null });

      const thrown = await apiFetch('/v1/playlists').catch((error: unknown) => error);

      expect(thrown).toMatchObject({
        status: 401,
        correlationId: expect.stringMatching(SERVER_ACCEPTED_ID),
      });
    });

    it('carries no correlation id on web, where none was sent', async () => {
      Platform.OS = 'web';
      __http.reply('GET /v1/playlists', { status: 500, json: {} });

      const thrown = await apiFetch('/v1/playlists').catch((error: unknown) => error);

      expect(thrown).toMatchObject({ status: 500 });
      expect((thrown as { correlationId?: string }).correlationId).toBeUndefined();
    });
  });
});

describe('failure logging', () => {
  const SECRET_TOKEN = 'secret-access-token';

  let warn: jest.SpyInstance;

  beforeEach(() => {
    getSession.mockReset();
    getSession.mockResolvedValue({
      data: { session: { access_token: SECRET_TOKEN } },
      error: null,
    });
    warn = jest.spyOn(console, 'warn').mockImplementation(() => {});
  });

  afterEach(() => {
    warn.mockRestore();
  });

  function loggedText(): string {
    return JSON.stringify(warn.mock.calls);
  }

  describe('apiFetch logs every failure at the point it throws', () => {
    it('logs method, endpoint, status and error code for a non-2xx response (a failed add-tracks-to-playlist)', async () => {
      __http.reply('POST /v1/playlists/p1/tracks', {
        status: 409,
        json: { code: 'playlist_full', detail: 'Road Trip Mix is full' },
      });

      await expect(
        apiFetch('/v1/playlists/p1/tracks', {
          method: 'POST',
          body: JSON.stringify({ track_ids: ['t1'] }),
        }),
      ).rejects.toMatchObject({ status: 409 });

      expect(warn).toHaveBeenCalledTimes(1);
      expect(warn).toHaveBeenCalledWith('[api] request failed', {
        method: 'POST',
        path: '/v1/playlists/p1/tracks',
        correlationId: expect.any(String),
        status: 409,
        code: 'playlist_full',
      });
    });

    it('logs the failure kind for a transport error', async () => {
      __http.fail('GET /v1/playlists');

      await expect(apiFetch('/v1/playlists')).rejects.toMatchObject({ name: 'NetworkError' });

      expect(warn).toHaveBeenCalledWith('[api] request failed', {
        method: 'GET',
        path: '/v1/playlists',
        correlationId: expect.any(String),
        failure: 'transport',
      });
    });

    it('logs a truncated success body as a transport failure', async () => {
      __http.reply('GET /v1/playlists', { status: 200, malformed: true });

      await expect(apiFetch('/v1/playlists')).rejects.toMatchObject({ name: 'NetworkError' });

      expect(warn).toHaveBeenCalledWith('[api] request failed', {
        method: 'GET',
        path: '/v1/playlists',
        correlationId: expect.any(String),
        failure: 'transport',
      });
    });

    it('logs a missing session as a 401 without calling the network', async () => {
      getSession.mockResolvedValue({ data: { session: null }, error: null });

      await expect(apiFetch('/v1/playlists')).rejects.toMatchObject({ status: 401 });

      expect(warn).toHaveBeenCalledWith('[api] request failed', {
        method: 'GET',
        path: '/v1/playlists',
        correlationId: expect.any(String),
        status: 401,
      });
    });

    it('never logs the query string, the request body, the server detail or the auth token', async () => {
      __http.reply('GET /v1/discovery/search', {
        status: 500,
        json: { code: 'internal', detail: 'boom for my private query' },
      });

      await expect(
        apiFetch('/v1/discovery/search?q=my%20private%20query', {
          headers: { 'X-Private': 'header-value' },
        }),
      ).rejects.toMatchObject({ status: 500 });

      const text = loggedText();
      expect(text).toContain('/v1/discovery/search');
      expect(text).not.toContain('private');
      expect(text).not.toContain('?q=');
      expect(text).not.toContain(SECRET_TOKEN);
      expect(text).not.toContain('Bearer');
      expect(text).not.toContain('header-value');
    });

    it('does not log a caller abort, which is a cancellation rather than a failure', async () => {
      __http.reply('GET /v1/playlists', { status: 200, json: {} });
      const controller = new AbortController();
      controller.abort();

      await expect(apiFetch('/v1/playlists', { signal: controller.signal })).rejects.toMatchObject({
        name: 'AbortError',
      });

      expect(warn).not.toHaveBeenCalled();
    });

    it('does not log a successful request', async () => {
      __http.reply('GET /v1/playlists', { status: 200, json: { items: [] } });

      await apiFetch('/v1/playlists');

      expect(warn).not.toHaveBeenCalled();
    });
  });

  // The session lookup is the one collaborator inside apiFetch that can throw an
  // arbitrary value: send() and readBody() convert whatever they catch into a
  // NetworkError first. So it is where an unrecognized throw is injected here.
  describe('apiFetch logs a failure whose class it does not recognize', () => {
    it('logs the class and the schema path of a ContractError', async () => {
      getSession.mockRejectedValue(new ContractError('Session.access_token', 'expected a string'));

      await expect(apiFetch('/v1/playlists')).rejects.toMatchObject({ name: 'ContractError' });

      expect(warn).toHaveBeenCalledWith('[api] request failed', {
        method: 'GET',
        path: '/v1/playlists',
        correlationId: expect.any(String),
        error: 'ContractError',
        at: 'Session.access_token',
      });
    });

    it('logs the class but never the message of any other Error', async () => {
      getSession.mockRejectedValue(new TypeError('boom for my private query'));

      await expect(apiFetch('/v1/discovery/search?q=my%20private%20query')).rejects.toMatchObject({
        name: 'TypeError',
      });

      expect(warn).toHaveBeenCalledWith('[api] request failed', {
        method: 'GET',
        path: '/v1/discovery/search',
        correlationId: expect.any(String),
        error: 'TypeError',
      });
      expect(loggedText()).not.toContain('private');
    });

    it('logs the type of a thrown value that is not an Error at all', async () => {
      getSession.mockRejectedValue('the session store is unavailable');

      await expect(apiFetch('/v1/playlists')).rejects.toBe('the session store is unavailable');

      expect(warn).toHaveBeenCalledWith('[api] request failed', {
        method: 'GET',
        path: '/v1/playlists',
        correlationId: expect.any(String),
        error: 'string',
      });
    });
  });
});

describe('stale session', () => {
  const { __http, fakeFetch } = require('../../../../jest/doubles/fetch.js');

  function withSession(accessToken = 'tok-a') {
    getSession.mockResolvedValue({
      data: { session: { access_token: accessToken } },
      error: null,
    });
  }

  function whileTheRequestIsInFlight(sessionChange: () => void): void {
    global.fetch = (async (url: RequestInfo | URL, init?: RequestInit) => {
      const response = await fakeFetch(url, init);
      sessionChange();
      return response;
    }) as typeof fetch;
  }

  function switchAccounts(): void {
    runSignOutCleanups();
  }

  let warn: jest.SpyInstance;

  beforeEach(() => {
    clearSessionExpired();
    getSession.mockReset();
    __http.reset();
    warn = jest.spyOn(console, 'warn').mockImplementation(() => {});
  });

  afterEach(() => {
    global.fetch = fakeFetch;
    warn.mockRestore();
  });

  describe('a 401 only expires the session whose credentials it was sent with', () => {
    it('a 401 that returns after a sign-out and a sign-in leaves the next user unblocked', async () => {
      withSession();
      __http.reply('GET /v1/library/tracks', { status: 401, json: { message: 'revoked' } });
      whileTheRequestIsInFlight(switchAccounts);

      await expect(apiFetch('/v1/library/tracks')).rejects.toMatchObject({ status: 401 });

      expect(getSessionExpired()).toBe(false);
    });

    it('a 401 for the pre-refresh token that returns after the same user refreshed leaves the session unblocked', async () => {
      withSession();
      __http.reply('GET /v1/library/tracks', { status: 401, json: { message: 'expired' } });
      whileTheRequestIsInFlight(renewSessionCredentials);

      await expect(apiFetch('/v1/library/tracks')).rejects.toMatchObject({ status: 401 });

      expect(getSessionExpired()).toBe(false);
    });

    it('a 401 for the credentials still in use expires the session', async () => {
      withSession();
      __http.reply('GET /v1/library/tracks', { status: 401, json: { message: 'revoked' } });

      await expect(apiFetch('/v1/library/tracks')).rejects.toMatchObject({ status: 401 });

      expect(getSessionExpired()).toBe(true);
    });

    it('a later 401 in the new session still expires it after a stale one was ignored', async () => {
      withSession();
      __http.reply('GET /v1/library/tracks', { status: 401, json: { message: 'revoked' } });
      whileTheRequestIsInFlight(switchAccounts);
      await expect(apiFetch('/v1/library/tracks')).rejects.toMatchObject({ status: 401 });
      global.fetch = fakeFetch;

      await expect(apiFetch('/v1/library/tracks')).rejects.toMatchObject({ status: 401 });

      expect(getSessionExpired()).toBe(true);
    });
  });
});

describe('transport failures', () => {
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
});

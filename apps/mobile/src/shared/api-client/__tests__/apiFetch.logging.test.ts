import { apiFetch } from '../index';
import { ContractError } from '@shared/errors';
import { supabase } from '@shared/auth/supabaseClient';

const { __http } = require('../../../../jest/doubles/fetch.js');

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn() } },
}));

const getSession = supabase.auth.getSession as jest.Mock;
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

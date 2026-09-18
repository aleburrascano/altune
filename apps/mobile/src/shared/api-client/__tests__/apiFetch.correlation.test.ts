import { Platform } from 'react-native';
import { apiFetch } from '../index';
import { supabase } from '@shared/auth/supabaseClient';

const { __http } = require('../../../../jest/doubles/fetch.js');

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn() } },
}));

const getSession = supabase.auth.getSession as jest.Mock;
// Mirrors the Go API's accepted shape (httputil.CorrelationID): <=64 chars of [A-Za-z0-9_-].
const SERVER_ACCEPTED_ID = /^[A-Za-z0-9_-]{1,64}$/;

let warn: jest.SpyInstance;
const originalOS = Platform.OS;

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

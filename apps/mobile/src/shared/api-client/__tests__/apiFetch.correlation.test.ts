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

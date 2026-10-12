import { audioRequestHeaders } from '@shared/apiClient/audio';
import { apiFetch } from '@shared/apiClient/index';
import { supabase } from '@shared/auth/supabaseClient';
import { clearSessionExpired } from '@shared/auth/sessionExpired';

const { __http } = require('@testing/doubles/fetch.js');

beforeEach(() => {
  clearSessionExpired();
  jest.spyOn(supabase.auth, 'getSession').mockResolvedValue({
    data: { session: { access_token: 'tok' } },
    error: null,
  } as never);
});

describe('request headers on web', () => {
  it('sends neither ngrok-skip-browser-warning nor X-Correlation-ID, which the CORS allowlist would reject', async () => {
    __http.reply('GET /v1/library/tracks', { status: 200, json: [] });

    await apiFetch('/v1/library/tracks');

    const { headers } = __http.last();
    expect(headers.Authorization).toBe('Bearer tok');
    expect(headers['ngrok-skip-browser-warning']).toBeUndefined();
    expect(headers['X-Correlation-ID']).toBeUndefined();
  });

  it('logs a failed request without a correlation id, since none was sent', async () => {
    const warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
    __http.reply('GET /v1/playlists', { status: 500, json: {} });

    await expect(apiFetch('/v1/playlists')).rejects.toMatchObject({ status: 500 });

    expect(warn).toHaveBeenCalledWith(
      '[api] request failed',
      expect.not.objectContaining({ correlationId: expect.anything() }),
    );
  });

  it('hands the caller an error with no correlation id', async () => {
    __http.reply('GET /v1/playlists', { status: 500, json: {} });

    const thrown = await apiFetch('/v1/playlists').catch((error: unknown) => error);

    expect(thrown).toMatchObject({ status: 500 });
    expect((thrown as { correlationId?: string }).correlationId).toBeUndefined();
  });

  it('omits the correlation header from the audio request headers', async () => {
    await expect(audioRequestHeaders()).resolves.toEqual({ Authorization: 'Bearer tok' });
  });
});

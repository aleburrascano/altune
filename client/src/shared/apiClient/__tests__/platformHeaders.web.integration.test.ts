import { audioRequestHeaders, apiFetch } from '@shared/apiClient';
import { clearSessionExpired } from '@shared/auth';
import { recordLogs } from '@testing/fakes/log/inMemoryLogger';
import { signedInWith } from '@shared/apiClient/__tests__/sessionLookup';
const { __http } = require('@testing/doubles/fetch.js');

const CORRELATION_ID_SHAPE = /^[0-9a-f]{16}$/;

beforeEach(() => {
  clearSessionExpired();
  signedInWith();
});

describe('request headers on web', () => {
  it('sends a correlation id, which the server allows, and no ngrok-skip-browser-warning', async () => {
    __http.reply('GET /v1/library/tracks', { status: 200, json: [] });

    await apiFetch('/v1/library/tracks');

    const { headers } = __http.last();
    expect(headers.Authorization).toBe('Bearer tok');
    expect(headers['ngrok-skip-browser-warning']).toBeUndefined();
    expect(headers['X-Correlation-ID']).toMatch(CORRELATION_ID_SHAPE);
  });

  it('logs a failed request with the correlation id it sent', async () => {
    const logs = recordLogs();
    __http.reply('GET /v1/playlists', { status: 500, json: {} });

    await expect(apiFetch('/v1/playlists')).rejects.toMatchObject({ status: 500 });

    expect(logs.warnings).toContainEqual([
      '[api] request failed',
      expect.objectContaining({ corr_id: __http.last().headers['X-Correlation-ID'] }),
    ]);
  });

  it('hands the caller an error carrying the correlation id', async () => {
    __http.reply('GET /v1/playlists', { status: 500, json: {} });

    const thrown = await apiFetch('/v1/playlists').catch((error: unknown) => error);

    expect(thrown).toMatchObject({
      status: 500,
      correlationId: __http.last().headers['X-Correlation-ID'],
    });
  });

  it('includes a correlation id in the audio request headers', async () => {
    await expect(audioRequestHeaders()).resolves.toEqual({
      Authorization: 'Bearer tok',
      'X-Correlation-ID': expect.stringMatching(CORRELATION_ID_SHAPE),
    });
  });
});

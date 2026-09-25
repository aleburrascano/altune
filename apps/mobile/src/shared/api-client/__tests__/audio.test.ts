import { Platform } from 'react-native';

import {
  audioStreamUrl,
  audioRequestHeaders,
  recoverAudio,
  fetchAudioUrls,
  isAudioPrefetchEnabled,
} from '../audio';
import { apiBase, ApiError, NetworkError } from '../index';
import { CORRELATION_HEADER } from '../correlationId';
import { ContractError } from '@shared/errors';
import { asTrackId, type TrackId } from '../ids';
import { supabase } from '@shared/auth/supabaseClient';
import { clearSessionExpired, getSessionExpired } from '@shared/auth/sessionExpired';

const { __http } = require('../../../../jest/doubles/fetch.js');

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn() } },
}));

const getSession = supabase.auth.getSession as jest.Mock;
// Mirrors the Go API's accepted shape (httputil.CorrelationID): <=64 chars of [A-Za-z0-9_-].
const SERVER_ACCEPTED_ID = /^[A-Za-z0-9_-]{1,64}$/;

function withSession(accessToken: string | null | undefined = 'tok') {
  getSession.mockResolvedValue({
    data: { session: accessToken == null ? null : { access_token: accessToken } },
    error: null,
  });
}

beforeEach(() => {
  getSession.mockReset();
  clearSessionExpired();
});

describe('audioStreamUrl', () => {
  it('builds the fallback stream URL from apiBase and the trackId', () => {
    expect(audioStreamUrl(asTrackId('t1'))).toBe(`${apiBase}/v1/tracks/t1/audio`);
  });

  it.each(['a/b', '.', '..', 'a?b=1', 'a#frag', ''])(
    'refuses a trackId %p smuggled past the brand instead of building another route',
    (id) => {
      expect(() => audioStreamUrl(id as TrackId)).toThrow(ContractError);
    },
  );

  it('never carries a credential in the returned URL', () => {
    expect(audioStreamUrl(asTrackId('t1'))).not.toContain('Bearer');
    expect(audioStreamUrl(asTrackId('t1'))).not.toContain('access_token');
  });
});

describe('audioRequestHeaders', () => {
  const originalOS = Platform.OS;
  let warn: jest.SpyInstance;

  beforeEach(() => {
    warn = jest.spyOn(console, 'warn').mockImplementation(() => {});
  });

  afterEach(() => {
    warn.mockRestore();
    Platform.OS = originalOS;
  });

  it('returns a Bearer Authorization header when a session with an access_token exists', async () => {
    withSession('secret-token');

    await expect(audioRequestHeaders()).resolves.toMatchObject({
      Authorization: 'Bearer secret-token',
    });
    expect(getSessionExpired()).toBe(false);
  });

  it('carries a correlation id the server accepts, fresh per call, so a stream failure can be matched to the server log lines', async () => {
    withSession();

    const first = await audioRequestHeaders();
    const second = await audioRequestHeaders();

    expect(first[CORRELATION_HEADER]).toMatch(SERVER_ACCEPTED_ID);
    expect(second[CORRELATION_HEADER]).toMatch(SERVER_ACCEPTED_ID);
    expect(second[CORRELATION_HEADER]).not.toBe(first[CORRELATION_HEADER]);
  });

  it('omits the correlation header on web, where the API CORS policy would reject the preflight', async () => {
    Platform.OS = 'web';
    withSession();

    await expect(audioRequestHeaders()).resolves.toEqual({ Authorization: 'Bearer tok' });
  });

  it('marks the session expired when there is no session, rather than handing the player unauthenticated headers in silence', async () => {
    withSession(null);

    const headers = await audioRequestHeaders();

    expect(headers.Authorization).toBeUndefined();
    expect(getSessionExpired()).toBe(true);
  });

  it('marks the session expired when the session object carries no access_token', async () => {
    getSession.mockResolvedValue({ data: { session: {} }, error: null });

    const headers = await audioRequestHeaders();

    expect(headers.Authorization).toBeUndefined();
    expect(getSessionExpired()).toBe(true);
  });

  it('leaves the session unexpired when the auth server itself is unreachable — an outage must not sign every listener out mid-playback', async () => {
    getSession.mockResolvedValue({
      data: { session: null },
      error: { name: 'AuthRetryableFetchError', message: 'network request failed' },
    });

    const headers = await audioRequestHeaders();

    expect(headers.Authorization).toBeUndefined();
    expect(getSessionExpired()).toBe(false);
  });

  it('logs the refused stream request against the correlation id it would have sent', async () => {
    withSession(null);

    const headers = await audioRequestHeaders();

    expect(warn).toHaveBeenCalledWith(
      '[api] request failed',
      expect.objectContaining({
        method: 'GET',
        path: '/v1/tracks/{id}/audio',
        correlationId: headers[CORRELATION_HEADER],
        status: 401,
      }),
    );
  });

  it('keeps the access token off audioStreamUrl even when both are used for the same track', async () => {
    withSession('leak-check-token');

    const headers = await audioRequestHeaders();
    const url = audioStreamUrl(asTrackId('t1'));

    expect(headers.Authorization).toBe('Bearer leak-check-token');
    expect(url).not.toContain('leak-check-token');
  });
});

describe('recoverAudio', () => {
  it('POSTs /v1/tracks/{id}/audio/recover', async () => {
    withSession();
    __http.reply('POST /v1/tracks/t1/audio/recover', { status: 202 });

    await recoverAudio(asTrackId('t1'));

    expect(__http.last().method).toBe('POST');
    expect(__http.last().path).toBe('/v1/tracks/t1/audio/recover');
  });

  it('resolves without throwing on the 202-with-empty-body success this endpoint returns', async () => {
    withSession();
    __http.reply('POST /v1/tracks/t1/audio/recover', { status: 202, malformed: true });

    await expect(recoverAudio(asTrackId('t1'))).resolves.toBeUndefined();
  });

  it("rejects with ApiError on a server failure, for the caller's .catch(() => {}) to swallow", async () => {
    withSession();
    __http.reply('POST /v1/tracks/t1/audio/recover', { status: 500, json: { message: 'boom' } });

    await expect(recoverAudio(asTrackId('t1'))).rejects.toBeInstanceOf(ApiError);
  });

  it('rejects with NetworkError on a dropped connection', async () => {
    withSession();
    __http.fail('POST /v1/tracks/t1/audio/recover');

    await expect(recoverAudio(asTrackId('t1'))).rejects.toBeInstanceOf(NetworkError);
  });

  it('never puts the access token in the recover request URL', async () => {
    withSession('leak-check-token');
    __http.reply('POST /v1/tracks/t1/audio/recover', { status: 202 });

    await recoverAudio(asTrackId('t1'));

    expect(__http.last().url).not.toContain('leak-check-token');
  });
});

describe('fetchAudioUrls', () => {
  it('tracks the server prefetch kill switch, ignoring a missing or non-boolean flag', async () => {
    withSession();
    const replyWith = (extra: Record<string, unknown>) =>
      __http.replyOnce('POST /v1/audio-urls', { status: 200, json: { urls: [], ...extra } });

    expect(isAudioPrefetchEnabled()).toBe(true);
    replyWith({ prefetch_enabled: false });
    await fetchAudioUrls(['t1']);
    expect(isAudioPrefetchEnabled()).toBe(false);
    replyWith({ prefetch_enabled: 'true' });
    await fetchAudioUrls(['t1']);
    replyWith({});
    await fetchAudioUrls(['t1']);
    expect(isAudioPrefetchEnabled()).toBe(false);
    replyWith({ prefetch_enabled: true });
    await fetchAudioUrls(['t1']);
    expect(isAudioPrefetchEnabled()).toBe(true);
  });

  it('maps the wire shape {track_id, url, version} to {trackId, url, version}', async () => {
    withSession();
    __http.reply('POST /v1/audio-urls', {
      status: 200,
      json: {
        urls: [{ track_id: 't1', url: 'https://cdn.example/t1.mp3', version: '1770000000000' }],
      },
    });

    await expect(fetchAudioUrls(['t1'])).resolves.toEqual([
      { trackId: 't1', url: 'https://cdn.example/t1.mp3', version: '1770000000000' },
    ]);
  });

  it('carries the version verbatim as an opaque token — it is compared, never parsed', async () => {
    withSession();
    __http.reply('POST /v1/audio-urls', {
      status: 200,
      json: {
        urls: [{ track_id: 't1', url: 'https://cdn.example/t1.mp3', version: 'not-a-number' }],
      },
    });

    await expect(fetchAudioUrls(['t1'])).resolves.toEqual([
      { trackId: 't1', url: 'https://cdn.example/t1.mp3', version: 'not-a-number' },
    ]);
  });

  it('falls back to an empty version when the server omits one, so a track never acquired since the column landed reads as "unknown" rather than crashing', async () => {
    withSession();
    __http.reply('POST /v1/audio-urls', {
      status: 200,
      json: { urls: [{ track_id: 't1', url: 'https://cdn.example/t1.mp3' }] },
    });

    await expect(fetchAudioUrls(['t1'])).resolves.toEqual([
      { trackId: 't1', url: 'https://cdn.example/t1.mp3', version: '' },
    ]);
  });

  it('maps several resolved urls in the order the server returned them', async () => {
    withSession();
    __http.reply('POST /v1/audio-urls', {
      status: 200,
      json: {
        urls: [
          { track_id: 't1', url: 'https://cdn.example/t1.mp3', version: 'v1' },
          { track_id: 't2', url: 'https://cdn.example/t2.mp3', version: 'v2' },
        ],
      },
    });

    await expect(fetchAudioUrls(['t1', 't2'])).resolves.toEqual([
      { trackId: 't1', url: 'https://cdn.example/t1.mp3', version: 'v1' },
      { trackId: 't2', url: 'https://cdn.example/t2.mp3', version: 'v2' },
    ]);
  });

  it('returns [] for an empty track id list without sending a request (dead branch — see FINDINGS)', async () => {
    withSession();

    await expect(fetchAudioUrls([])).resolves.toEqual([]);
    expect(__http.requests.length).toBe(0);
  });

  it('POSTs { track_ids } with a JSON Content-Type', async () => {
    withSession();
    __http.reply('POST /v1/audio-urls', { status: 200, json: { urls: [] } });

    await fetchAudioUrls(['t1', 't2']);

    const request = __http.last();
    expect(request.method).toBe('POST');
    expect(request.headers['Content-Type']).toBe('application/json');
    expect(JSON.parse(request.body)).toEqual({ track_ids: ['t1', 't2'] });
  });

  it('rejects with ApiError on a server failure', async () => {
    withSession();
    __http.reply('POST /v1/audio-urls', { status: 500, json: {} });

    await expect(fetchAudioUrls(['t1'])).rejects.toBeInstanceOf(ApiError);
  });

  it('rejects with NetworkError on a truncated body', async () => {
    withSession();
    __http.reply('POST /v1/audio-urls', { status: 200, malformed: true });

    await expect(fetchAudioUrls(['t1'])).rejects.toBeInstanceOf(NetworkError);
  });

  it('rejects with NetworkError on a dropped connection', async () => {
    withSession();
    __http.fail('POST /v1/audio-urls');

    await expect(fetchAudioUrls(['t1'])).rejects.toBeInstanceOf(NetworkError);
  });

  it('never leaks the access token into the request URL', async () => {
    withSession('leak-check-token');
    __http.reply('POST /v1/audio-urls', { status: 200, json: { urls: [] } });

    await fetchAudioUrls(['t1']);

    expect(__http.last().url).not.toContain('leak-check-token');
  });

  describe('the internal 2500ms timeout', () => {
    beforeEach(() => jest.useFakeTimers());
    afterEach(() => jest.useRealTimers());

    it('is still in flight at 2499ms — nothing has settled yet', async () => {
      withSession();
      __http.hang('POST /v1/audio-urls');

      const pending = fetchAudioUrls(['t1']);
      const caught = pending.catch((e: unknown) => e);
      await Promise.resolve();
      await Promise.resolve();
      await Promise.resolve();

      jest.advanceTimersByTime(2499);
      let settled = false;
      caught.then(() => (settled = true));
      await Promise.resolve();
      await Promise.resolve();

      expect(settled).toBe(false);
    });

    it('aborts at exactly 2500ms and rejects with an AbortError', async () => {
      withSession();
      __http.hang('POST /v1/audio-urls');

      const pending = fetchAudioUrls(['t1']);
      const caught = pending.catch((e: unknown) => e);
      await Promise.resolve();
      await Promise.resolve();
      await Promise.resolve();

      jest.advanceTimersByTime(2500);

      const error = await caught;
      expect((error as Error).name).toBe('AbortError');
    });

    it('clears the timeout on a success well inside the window (no leaked timer)', async () => {
      withSession();
      __http.reply('POST /v1/audio-urls', { status: 200, json: { urls: [] } });

      await fetchAudioUrls(['t1']);

      expect(jest.getTimerCount()).toBe(0);
    });

    it('clears the timeout when the request throws (no leaked timer on the failure path)', async () => {
      withSession();
      __http.reply('POST /v1/audio-urls', { status: 500, json: {} });

      await expect(fetchAudioUrls(['t1'])).rejects.toBeInstanceOf(ApiError);

      expect(jest.getTimerCount()).toBe(0);
    });
  });
});

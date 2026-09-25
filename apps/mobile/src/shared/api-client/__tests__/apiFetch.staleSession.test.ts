import { apiFetch } from '../index';
import { audioRequestHeaders } from '../audio';
import { supabase } from '@shared/auth/supabaseClient';
import {
  clearSessionExpired,
  getSessionExpired,
  renewSessionCredentials,
} from '@shared/auth/sessionExpired';
import { runSignOutCleanups } from '@shared/session/signOutCleanup';

const { __http, fakeFetch } = require('../../../../jest/doubles/fetch.js');

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn() } },
}));

const getSession = supabase.auth.getSession as jest.Mock;

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

describe('the audio stream header path fences its session refusal the same way', () => {
  it('a refused session read that settles after a sign-out and a sign-in leaves the next user unblocked', async () => {
    getSession.mockImplementation(async () => {
      switchAccounts();
      return {
        data: { session: null },
        error: { name: 'AuthApiError', message: 'refresh_token_not_found' },
      };
    });

    await expect(audioRequestHeaders()).resolves.not.toHaveProperty('Authorization');

    expect(getSessionExpired()).toBe(false);
  });

  it('a refused session read in the session that asked expires it', async () => {
    getSession.mockResolvedValue({
      data: { session: null },
      error: { name: 'AuthApiError', message: 'refresh_token_not_found' },
    });

    await expect(audioRequestHeaders()).resolves.not.toHaveProperty('Authorization');

    expect(getSessionExpired()).toBe(true);
  });
});

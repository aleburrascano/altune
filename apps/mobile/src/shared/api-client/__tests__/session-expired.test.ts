/**
 * The 401 feedback edge: apiFetch marks the session expired when the backend
 * rejects a token the SDK still considers valid.
 *
 * Without this there is no path from HTTP 401 back to auth state — the SDK
 * never emits SIGNED_OUT, AuthGate keeps rendering the app, and every query
 * fails forever with no recovery but finding Sign Out in Settings.
 */
/* eslint-disable @typescript-eslint/no-require-imports */

const mockGetSession = jest.fn(async () => ({
  data: { session: null as null | { access_token: string } },
}));
jest.mock('../../auth/supabaseClient', () => ({
  supabase: {
    auth: { getSession: () => mockGetSession() },
  },
}));

const mockFetch = jest.fn();
beforeAll(() => {
  (global as unknown as { fetch: typeof mockFetch }).fetch = mockFetch;
});

beforeEach(() => {
  jest.resetModules();
  mockGetSession.mockReset().mockResolvedValue({ data: { session: { access_token: 'AT' } } });
  mockFetch.mockReset();
});

describe('session-expired marking', () => {
  it('marks the session expired when the backend returns 401', async () => {
    mockFetch.mockResolvedValue({ ok: false, status: 401, json: async () => ({}) });
    const { apiFetch } = require('../index');
    const { getSessionExpired } = require('../../auth/sessionExpired');

    expect(getSessionExpired()).toBe(false);
    await expect(apiFetch('/v1/tracks')).rejects.toThrow();
    expect(getSessionExpired()).toBe(true);
  });

  it('does not mark on other error statuses', async () => {
    // A 500 is the backend having a bad day, not a dead session — bouncing the
    // user to sign-in over it would be wrong.
    mockFetch.mockResolvedValue({ ok: false, status: 500, json: async () => ({}) });
    const { apiFetch } = require('../index');
    const { getSessionExpired } = require('../../auth/sessionExpired');

    await expect(apiFetch('/v1/tracks')).rejects.toThrow();
    expect(getSessionExpired()).toBe(false);
  });

  it('does not mark when there is no local session (the gate handles that)', async () => {
    // No session => AuthGate already redirects to /sign-in. Marking here would
    // show the expired notice instead of the sign-in screen.
    mockGetSession.mockResolvedValue({ data: { session: null } });
    const { apiFetch } = require('../index');
    const { getSessionExpired } = require('../../auth/sessionExpired');

    await expect(apiFetch('/v1/tracks')).rejects.toThrow();
    expect(getSessionExpired()).toBe(false);
    expect(mockFetch).not.toHaveBeenCalled();
  });

  it('clearSessionExpired resets the flag', async () => {
    mockFetch.mockResolvedValue({ ok: false, status: 401, json: async () => ({}) });
    const { apiFetch } = require('../index');
    const { getSessionExpired, clearSessionExpired } = require('../../auth/sessionExpired');

    await expect(apiFetch('/v1/tracks')).rejects.toThrow();
    expect(getSessionExpired()).toBe(true);

    clearSessionExpired();
    expect(getSessionExpired()).toBe(false);
  });
});

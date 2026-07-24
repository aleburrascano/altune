
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
    mockFetch.mockResolvedValue({ ok: false, status: 500, json: async () => ({}) });
    const { apiFetch } = require('../index');
    const { getSessionExpired } = require('../../auth/sessionExpired');

    await expect(apiFetch('/v1/tracks')).rejects.toThrow();
    expect(getSessionExpired()).toBe(false);
  });

  it('does not mark when there is no local session (the gate handles that)', async () => {
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

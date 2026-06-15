/**
 * apiFetch Authorization header injection (Slice 13).
 *
 * Verifies the Bearer header is attached when a Supabase session exists,
 * omitted when no session, and that custom headers passed in init are
 * preserved alongside.
 */
/* eslint-disable @typescript-eslint/no-require-imports */

const mockGetSession = jest.fn(async () => ({ data: { session: null as null | { access_token: string } } }));
jest.mock('../../auth/supabaseClient', () => ({
  supabase: {
    auth: { getSession: () => mockGetSession() },
  },
}));

const mockFetch = jest.fn();
beforeAll(() => {
  // Override the global fetch the wrapper uses.
  (global as unknown as { fetch: typeof mockFetch }).fetch = mockFetch;
});

beforeEach(() => {
  mockGetSession.mockReset().mockResolvedValue({ data: { session: null } });
  mockFetch.mockReset().mockResolvedValue({
    ok: true,
    json: async () => ({}),
  });
});

describe('apiFetch auth header injection', () => {
  it('injects Bearer when session has an access_token', async () => {
    mockGetSession.mockResolvedValueOnce({
      data: { session: { access_token: 'AT123' } },
    });
    const { apiFetch } = require('../index');

    await apiFetch('/v1/tracks');

    const call = mockFetch.mock.calls[0] as [string, RequestInit];
    const headers = call[1].headers as Record<string, string>;
    expect(headers.Authorization).toBe('Bearer AT123');
  });

  it('omits Authorization when session is null', async () => {
    mockGetSession.mockResolvedValueOnce({ data: { session: null } });
    const { apiFetch } = require('../index');

    await apiFetch('/v1/tracks');

    const call = mockFetch.mock.calls[0] as [string, RequestInit];
    const headers = call[1].headers as Record<string, string>;
    expect(headers.Authorization).toBeUndefined();
  });

  it('preserves custom headers alongside Authorization', async () => {
    mockGetSession.mockResolvedValueOnce({
      data: { session: { access_token: 'AT456' } },
    });
    const { apiFetch } = require('../index');

    await apiFetch('/v1/tracks', { headers: { 'X-Trace-Id': 'abc' } });

    const call = mockFetch.mock.calls[0] as [string, RequestInit];
    const headers = call[1].headers as Record<string, string>;
    expect(headers.Authorization).toBe('Bearer AT456');
    expect(headers['X-Trace-Id']).toBe('abc');
    expect(headers['ngrok-skip-browser-warning']).toBe('1');
  });
});

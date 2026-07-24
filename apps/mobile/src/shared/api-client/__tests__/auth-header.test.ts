
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

  it('fails fast without a request when session is null', async () => {
    mockGetSession.mockResolvedValueOnce({ data: { session: null } });
    const { apiFetch, ApiError } = require('../index');

    await expect(apiFetch('/v1/tracks')).rejects.toThrow(ApiError);
    expect(mockFetch).not.toHaveBeenCalled();
  });

  it('fails fast when getSession reports a stale refresh token', async () => {
    mockGetSession.mockResolvedValueOnce({
      data: { session: null },
      error: { message: 'Invalid Refresh Token' },
    } as never);
    const { apiFetch } = require('../index');

    await expect(apiFetch('/v1/tracks')).rejects.toThrow(/Invalid Refresh Token/);
    expect(mockFetch).not.toHaveBeenCalled();
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

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

function abortError(): Error {
  const error = new Error('Aborted');
  error.name = 'AbortError';
  return error;
}

function hangUntilAborted(): void {
  mockFetch.mockImplementation(
    (_url: string, init: RequestInit) =>
      new Promise((_resolve, reject) => {
        const signal = init.signal;
        if (signal == null) return;
        if (signal.aborted) {
          reject(abortError());
          return;
        }
        signal.addEventListener('abort', () => reject(abortError()));
      }),
  );
}

async function waitForRequest(): Promise<void> {
  for (let tick = 0; tick < 50 && mockFetch.mock.calls.length === 0; tick += 1) {
    await Promise.resolve();
  }
}

describe('a failed session refresh is a network failure, not a 401', () => {
  it('throws NetworkError when getSession cannot reach the auth server', async () => {
    mockGetSession.mockResolvedValue({
      data: { session: null },
      error: { name: 'AuthRetryableFetchError', message: 'Network request failed', status: 0 },
    } as never);
    const { apiFetch, NetworkError } = require('../index');

    await expect(apiFetch('/v1/library/albums')).rejects.toThrow(NetworkError);
    expect(mockFetch).not.toHaveBeenCalled();
  });

  it('that failure is retryable, so the library query comes back', async () => {
    mockGetSession.mockResolvedValue({
      data: { session: null },
      error: { name: 'AuthRetryableFetchError', message: 'Network request failed', status: 0 },
    } as never);
    const { apiFetch, isRetryable } = require('../index');

    const error = await apiFetch('/v1/library/albums').catch((e: unknown) => e);

    expect(isRetryable(error)).toBe(true);
  });

  it('still throws a non-retryable 401 when the refresh token is genuinely stale', async () => {
    mockGetSession.mockResolvedValue({
      data: { session: null },
      error: { name: 'AuthApiError', message: 'Invalid Refresh Token', status: 400 },
    } as never);
    const { apiFetch, ApiError, isRetryable } = require('../index');

    const error = await apiFetch('/v1/library/albums').catch((e: unknown) => e);

    expect(error).toBeInstanceOf(ApiError);
    expect(error.status).toBe(401);
    expect(isRetryable(error)).toBe(false);
  });
});

describe('transport failures', () => {
  it('maps a rejected fetch to a retryable NetworkError', async () => {
    mockFetch.mockRejectedValue(new TypeError('Network request failed'));
    const { apiFetch, NetworkError, isRetryable } = require('../index');

    const error = await apiFetch('/v1/tracks').catch((e: unknown) => e);

    expect(error).toBeInstanceOf(NetworkError);
    expect(error.failure).toBe('transport');
    expect(isRetryable(error)).toBe(true);
  });

  it('maps a truncated body to a retryable NetworkError', async () => {
    mockFetch.mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => {
        throw new SyntaxError('Unexpected end of JSON input');
      },
    });
    const { apiFetch, NetworkError, isRetryable } = require('../index');

    const error = await apiFetch('/v1/tracks').catch((e: unknown) => e);

    expect(error).toBeInstanceOf(NetworkError);
    expect(isRetryable(error)).toBe(true);
  });
});

describe('every request carries a deadline', () => {
  beforeEach(() => {
    jest.useFakeTimers();
  });
  afterEach(() => {
    jest.useRealTimers();
  });

  it('passes an abort signal to fetch', async () => {
    mockFetch.mockResolvedValue({ ok: true, status: 200, json: async () => ({}) });
    const { apiFetch } = require('../index');

    await apiFetch('/v1/tracks');

    const init = mockFetch.mock.calls[0][1] as RequestInit;
    expect(init.signal).toBeDefined();
  });

  it('gives up on a hung request instead of spinning forever', async () => {
    hangUntilAborted();
    const { apiFetch, NetworkError, REQUEST_TIMEOUT_MS } = require('../index');

    const pending = apiFetch('/v1/library/albums').catch((e: unknown) => e);
    await waitForRequest();
    jest.advanceTimersByTime(REQUEST_TIMEOUT_MS);

    const error = await pending;
    expect(error).toBeInstanceOf(NetworkError);
    expect(error.failure).toBe('timeout');
  });

  it('clears the timer once a request succeeds', async () => {
    mockFetch.mockResolvedValue({ ok: true, status: 200, json: async () => ({}) });
    const { apiFetch } = require('../index');

    await apiFetch('/v1/tracks');

    expect(jest.getTimerCount()).toBe(0);
  });
});

describe('caller cancellation stays cancellation', () => {
  it('rethrows the abort instead of relabelling it a network failure', async () => {
    hangUntilAborted();
    const { apiFetch, NetworkError } = require('../index');
    const controller = new AbortController();

    const pending = apiFetch('/v1/discovery/search?q=x', {
      signal: controller.signal,
    }).catch((e: unknown) => e);
    await waitForRequest();
    controller.abort();

    const error = await pending;
    expect(error).not.toBeInstanceOf(NetworkError);
    expect((error as Error).name).toBe('AbortError');
  });

  it('does not fire a request when the caller signal is already aborted', async () => {
    hangUntilAborted();
    const { apiFetch, NetworkError } = require('../index');
    const controller = new AbortController();
    controller.abort();

    const error = await apiFetch('/v1/tracks', { signal: controller.signal }).catch(
      (e: unknown) => e,
    );

    expect(error).not.toBeInstanceOf(NetworkError);
  });
});

describe('isRetryable', () => {
  it.each([
    [500, true],
    [502, true],
    [503, true],
    [429, true],
    [404, false],
    [400, false],
    [401, false],
  ])('status %i -> %s', (status, expected) => {
    const { ApiError, isRetryable } = require('../errors');
    expect(isRetryable(new ApiError(status, 'x'))).toBe(expected);
  });

  it('does not retry an unknown error', () => {
    const { isRetryable } = require('../errors');
    expect(isRetryable(new Error('boom'))).toBe(false);
  });
});

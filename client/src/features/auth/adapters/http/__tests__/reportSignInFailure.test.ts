import { apiBase } from '@shared/apiClient';
import { appVersion } from '@shared/device/device';
import { applyKillSwitches } from '@shared/killSwitch/killSwitch';
import { _resetOutboxForTest, flushOutbox } from '@shared/telemetry/outbox';

import { reportSignInFailure } from '@features/auth/adapters/http/reportSignInFailure';
import { thrownErrorDetail } from '@features/auth/domain/authError';

import { createSupabaseAuthMock } from '@features/auth/__tests__/testUtils/authTestUtils';

const { getSession } = createSupabaseAuthMock('getSession');

const realFetch = global.fetch;
const fetchMock = jest.fn();

beforeEach(() => {
  fetchMock.mockReset().mockResolvedValue({ status: 204 });
  global.fetch = fetchMock as unknown as typeof fetch;
  getSession.mockResolvedValue({ data: { session: { access_token: 'tok' } }, error: null });
  _resetOutboxForTest();
});

afterEach(() => {
  global.fetch = realFetch;
  applyKillSwitches({ telemetry_enabled: true });
});

describe('reportSignInFailure: the anonymous pre-session report', () => {
  it('posts exactly the reason and app version, with no Authorization header', () => {
    reportSignInFailure('invalid_credentials');

    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe(`${apiBase}/v1/public/auth-failures`);
    expect(init.method).toBe('POST');
    expect(JSON.parse(init.body)).toEqual({
      reason: 'invalid_credentials',
      app_version: appVersion(),
    });
    expect(Object.keys(init.headers).map((h) => h.toLowerCase())).toEqual(['content-type']);
  });

  it.each([
    ['a rejected fetch', () => fetchMock.mockRejectedValue(new Error('offline'))],
    ['a 429', () => fetchMock.mockResolvedValue({ status: 429 })],
    ['a 500', () => fetchMock.mockResolvedValue({ status: 500 })],
  ])('swallows %s and never retries or queues it', async (_name, arrange) => {
    arrange();

    expect(() => reportSignInFailure('network')).not.toThrow();
    await new Promise((resolve) => setTimeout(resolve, 0));

    await flushOutbox();

    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it('swallows a synchronous throw from fetch', () => {
    fetchMock.mockImplementation(() => {
      throw new Error('boom');
    });

    expect(() => reportSignInFailure('unknown')).not.toThrow();
  });

  it('posts nothing when the telemetryFlush kill switch is off', () => {
    applyKillSwitches({ telemetry_enabled: false });

    reportSignInFailure('unknown');

    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('aborts a hanging request after 5 seconds', () => {
    jest.useFakeTimers();
    fetchMock.mockReturnValue(new Promise(() => undefined));

    reportSignInFailure('network');
    const signal: AbortSignal = fetchMock.mock.calls[0][1].signal;
    expect(signal.aborted).toBe(false);
    jest.advanceTimersByTime(5_000);

    expect(signal.aborted).toBe(true);
    jest.useRealTimers();
  });
});

describe('reportSignInFailure', () => {
  afterEach(() => {
    jest.restoreAllMocks();
  });

  it('logs a warning with the reason and error detail when the report is not delivered', async () => {
    const error = new Error('offline');
    jest.spyOn(global, 'fetch').mockRejectedValue(error);
    const warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);

    reportSignInFailure('network');
    await new Promise((resolve) => setImmediate(resolve));

    expect(warn).toHaveBeenCalledWith('[auth] sign-in failure report not delivered', {
      reason: 'network',
      ...thrownErrorDetail(error),
    });
  });
});

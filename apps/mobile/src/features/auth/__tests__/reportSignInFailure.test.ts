import { apiBase } from '@shared/api-client';
import { appVersion } from '@shared/device/device';
import { applyKillSwitches } from '@shared/killSwitch/killSwitch';
import { enqueueCritical } from '@shared/telemetry/outbox';
import { recordEvent } from '@shared/telemetry/recordEvent';

import { reportSignInFailure } from '../reportSignInFailure';

jest.mock('@shared/telemetry/outbox', () => ({ enqueueCritical: jest.fn() }));
jest.mock('@shared/telemetry/recordEvent', () => ({ recordEvent: jest.fn() }));

const realFetch = global.fetch;
const fetchMock = jest.fn();

beforeEach(() => {
  fetchMock.mockReset().mockResolvedValue({ status: 204 });
  global.fetch = fetchMock as unknown as typeof fetch;
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

    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(recordEvent).not.toHaveBeenCalled();
    expect(enqueueCritical).not.toHaveBeenCalled();
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

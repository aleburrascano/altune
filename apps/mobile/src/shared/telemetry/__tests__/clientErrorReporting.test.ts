import { apiFetch } from '@shared/api-client';
import { supabase } from '@shared/auth/supabaseClient';
import { enqueueCritical } from '../outbox';
import {
  _resetGlobalErrorReportingForTest,
  installGlobalErrorReporting,
  reportClientError,
} from '../clientErrorReporting';

const { __http } = require('../../../../jest/doubles/fetch.js');

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn() } },
}));

jest.mock('../outbox', () => ({ enqueueCritical: jest.fn() }));

const mockPreviousHandler = jest.fn();
const mockSetGlobalHandler = jest.fn();
jest.mock('react-native/Libraries/vendor/core/ErrorUtils', () => ({
  getGlobalHandler: () => mockPreviousHandler,
  setGlobalHandler: (fn: unknown) => mockSetGlobalHandler(fn),
}));

const mockEnableRejectionTracking = jest.fn();
const globalWithHermes = globalThis as unknown as { HermesInternal?: unknown; __DEV__: boolean };
const devMode = globalWithHermes.__DEV__;

let mockExpoConfig: { version?: string } | undefined = { version: '1.2.3' };
jest.mock('expo-constants', () => ({
  __esModule: true,
  default: {
    get expoConfig() {
      return mockExpoConfig;
    },
  },
}));

const enqueueCriticalMock = enqueueCritical as jest.MockedFunction<typeof enqueueCritical>;

function lastPayload(): Record<string, unknown> {
  const call = enqueueCriticalMock.mock.calls.at(-1);
  return (call?.[0]?.payload ?? {}) as Record<string, unknown>;
}

beforeEach(() => {
  enqueueCriticalMock.mockReset().mockResolvedValue(undefined);
  mockSetGlobalHandler.mockReset();
  mockEnableRejectionTracking.mockReset();
  globalWithHermes.HermesInternal = { enablePromiseRejectionTracker: mockEnableRejectionTracking };
  globalWithHermes.__DEV__ = false;
  mockPreviousHandler.mockReset();
  mockExpoConfig = { version: '1.2.3' };
  _resetGlobalErrorReportingForTest();
});

afterAll(() => {
  _resetGlobalErrorReportingForTest();
});

afterEach(() => {
  delete globalWithHermes.HermesInternal;
  globalWithHermes.__DEV__ = devMode;
});

describe('reportClientError', () => {
  it('carries a trimmed message, stack and the app version', () => {
    reportClientError(new Error('boom'), 'uncaught');

    expect(enqueueCriticalMock).toHaveBeenCalledWith(
      expect.objectContaining({ type: 'client_error' }),
    );
    const payload = lastPayload();
    expect(payload['source']).toBe('uncaught');
    expect(payload['message']).toBe('boom');
    expect(typeof payload['stack']).toBe('string');
    expect(payload['app_version']).toEqual(expect.any(String));
  });

  it('trims a message past the size cap', () => {
    reportClientError(new Error('x'.repeat(1000)), 'boundary');

    const message = lastPayload()['message'] as string;
    expect(message.length).toBeLessThan(600);
    expect(message.endsWith('…')).toBe(true);
  });

  it('stringifies a non-Error thrown value, with no stack', () => {
    reportClientError('a plain string throw', 'unhandled_rejection');

    const payload = lastPayload();
    expect(payload['message']).toBe('a plain string throw');
    expect('stack' in payload).toBe(false);
  });

  it('carries the configured app version', () => {
    reportClientError(new Error('boom'), 'uncaught');

    expect(lastPayload()['app_version']).toBe('1.2.3');
  });

  it('falls back to "dev" when no app version is configured', () => {
    mockExpoConfig = undefined;

    reportClientError(new Error('boom'), 'uncaught');

    expect(lastPayload()['app_version']).toBe('dev');
  });
});

describe('reportClientError dedupe', () => {
  const WINDOW_MS = 60_000;
  let now = 1_000_000;

  beforeEach(() => {
    now = 1_000_000;
    jest.spyOn(Date, 'now').mockImplementation(() => now);
  });

  afterEach(() => {
    jest.restoreAllMocks();
  });

  it('enqueues one client_error for the same error repeated inside the window', () => {
    const error = new Error('render loop');
    for (let i = 0; i < 5; i += 1) reportClientError(error, 'boundary');

    expect(enqueueCriticalMock).toHaveBeenCalledTimes(1);
  });

  it('enqueues distinct errors and the same message from another source', () => {
    reportClientError(new Error('first'), 'boundary');
    reportClientError(new Error('second'), 'boundary');
    reportClientError(new Error('first'), 'uncaught');

    expect(enqueueCriticalMock).toHaveBeenCalledTimes(3);
  });

  it('reports again after the window, carrying the count of suppressed repeats', () => {
    const error = new Error('render loop');
    for (let i = 0; i < 4; i += 1) reportClientError(error, 'boundary');
    now += WINDOW_MS;
    reportClientError(error, 'boundary');

    expect(enqueueCriticalMock).toHaveBeenCalledTimes(2);
    expect(lastPayload()['suppressed_repeats']).toBe(3);
  });

  it('omits the count when nothing was suppressed', () => {
    reportClientError(new Error('once'), 'boundary');

    expect('suppressed_repeats' in lastPayload()).toBe(false);
  });

  it('keeps reporting every distinct error past the tracking cap', () => {
    for (let i = 0; i < 120; i += 1) reportClientError(new Error(`distinct ${i}`), 'boundary');

    expect(enqueueCriticalMock).toHaveBeenCalledTimes(120);
  });

  it('reports again when the clock is moved backwards', () => {
    const error = new Error('render loop');
    reportClientError(error, 'boundary');
    now -= 3_600_000;
    reportClientError(error, 'boundary');

    expect(enqueueCriticalMock).toHaveBeenCalledTimes(2);
  });

  it('delivers the count of a burst that stops inside the window when the window ends', () => {
    jest.useFakeTimers({ doNotFake: ['Date'] });
    const error = new Error('render loop');
    for (let i = 0; i < 4; i += 1) reportClientError(error, 'boundary');
    enqueueCriticalMock.mockClear();

    now += WINDOW_MS;
    jest.advanceTimersByTime(WINDOW_MS);

    expect(enqueueCriticalMock).toHaveBeenCalledTimes(1);
    expect(lastPayload()['suppressed_repeats']).toBe(3);
    jest.useRealTimers();
  });

  it('does not report a burst count twice', () => {
    jest.useFakeTimers({ doNotFake: ['Date'] });
    const error = new Error('render loop');
    for (let i = 0; i < 4; i += 1) reportClientError(error, 'boundary');
    now += WINDOW_MS;
    reportClientError(error, 'boundary');
    enqueueCriticalMock.mockClear();

    jest.advanceTimersByTime(WINDOW_MS);

    expect(enqueueCriticalMock).not.toHaveBeenCalled();
    jest.useRealTimers();
  });

  it('keeps the hot repeating error suppressed while distinct errors fill the cap', () => {
    const hot = new Error('hot');
    reportClientError(hot, 'boundary');
    reportClientError(hot, 'boundary');
    for (let i = 0; i < 120; i += 1) reportClientError(new Error(`distinct ${i}`), 'boundary');
    enqueueCriticalMock.mockClear();

    reportClientError(hot, 'boundary');

    expect(enqueueCriticalMock).not.toHaveBeenCalled();
  });

  it('treats messages that differ only past the size cap as the same error', () => {
    reportClientError(new Error(`${'x'.repeat(600)}a`), 'boundary');
    reportClientError(new Error(`${'x'.repeat(600)}b`), 'boundary');

    expect(enqueueCriticalMock).toHaveBeenCalledTimes(1);
  });

  it('tells apart errors that differ only in their stack head', () => {
    const first = new Error('same');
    first.stack = 'Error: same\n    at first (a.js:1:1)';
    const second = new Error('same');
    second.stack = 'Error: same\n    at second (b.js:2:2)';
    reportClientError(first, 'boundary');
    reportClientError(second, 'boundary');

    expect(enqueueCriticalMock).toHaveBeenCalledTimes(2);
  });
});

describe('installGlobalErrorReporting', () => {
  it('wires the global handler and rejection tracking exactly once', () => {
    installGlobalErrorReporting();
    installGlobalErrorReporting();

    expect(mockSetGlobalHandler).toHaveBeenCalledTimes(1);
    expect(mockEnableRejectionTracking).toHaveBeenCalledTimes(1);
  });

  it('reports an uncaught error and forwards it to the previous handler', () => {
    installGlobalErrorReporting();
    const installedHandler = mockSetGlobalHandler.mock.calls[0]?.[0] as (
      error: unknown,
      isFatal: boolean,
    ) => void;

    const error = new Error('native crash');
    installedHandler(error, true);

    expect(lastPayload()['source']).toBe('uncaught');
    expect(mockPreviousHandler).toHaveBeenCalledWith(error, true);
  });

  it('reports an unhandled rejection', () => {
    installGlobalErrorReporting();
    const onUnhandled = mockEnableRejectionTracking.mock.calls[0]?.[0]?.onUnhandled as (
      id: number,
      error: unknown,
    ) => void;

    onUnhandled(1, new Error('dropped promise'));

    expect(lastPayload()['source']).toBe('unhandled_rejection');
  });

  it('reports nothing when a rejection is handled late', () => {
    installGlobalErrorReporting();
    const onHandled = mockEnableRejectionTracking.mock.calls[0]?.[0]?.onHandled as (
      id: number,
    ) => void;

    onHandled(1);

    expect(enqueueCriticalMock).not.toHaveBeenCalled();
  });

  it('leaves dev builds on the built-in rejection warnings', () => {
    globalWithHermes.__DEV__ = true;

    installGlobalErrorReporting();

    expect(mockEnableRejectionTracking).not.toHaveBeenCalled();
    expect(mockSetGlobalHandler).toHaveBeenCalledTimes(1);
  });

  it('skips rejection tracking when the runtime is not Hermes', () => {
    delete globalWithHermes.HermesInternal;

    expect(() => installGlobalErrorReporting()).not.toThrow();
    expect(mockSetGlobalHandler).toHaveBeenCalledTimes(1);
  });
});

describe('reportClientError with a failed search request', () => {
  it('ships a payload that carries no search text', async () => {
    jest.spyOn(console, 'warn').mockImplementation(() => undefined);
    (supabase.auth.getSession as jest.Mock).mockResolvedValue({
      data: { session: { access_token: 'tok' } },
      error: null,
    });
    __http.fail('GET /v1/discovery/search');

    const error = await apiFetch('/v1/discovery/search?q=secret').catch((e: unknown) => e);
    reportClientError(error, 'unhandled_rejection');

    expect(JSON.stringify(lastPayload())).not.toContain('secret');
  });
});

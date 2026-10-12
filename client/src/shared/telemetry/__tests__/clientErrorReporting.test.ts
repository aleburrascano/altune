import { apiFetch } from '@shared/apiClient';
import { signInTestUser } from '@testing/signInTestUser';
import {
  _resetGlobalErrorReportingForTest,
  installGlobalErrorReporting,
  reportClientError,
} from '@shared/telemetry/clientErrorReporting';

const { __http } = require('@testing/doubles/fetch.js');

jest.mock('@supabase/supabase-js', () => require('@testing/doubles/supabase-js.js'));

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

type DeliveredEvent = { type: string; payload: Record<string, unknown> };

function delivered(): DeliveredEvent[] {
  return (__http.requests as { method: string; path: string; body: string }[])
    .filter((request) => request.method === 'POST' && request.path === '/v1/discovery/events')
    .map((request) => JSON.parse(request.body) as DeliveredEvent)
    .filter((event) => event.type === 'client_error');
}

function lastPayload(): Record<string, unknown> {
  const last = delivered().at(-1);
  if (last === undefined) throw new Error('no client_error was delivered');
  return last.payload;
}

const DISTINCT_ERRORS = 120;
const TWO_REPORTS = 2;
const THREE_REPORTS = 3;

async function settle(): Promise<void> {
  await new Promise((resolve) => setImmediate(resolve));
}

async function forgetDelivered(): Promise<void> {
  await settle();
  __http.requests.length = 0;
}

const FAKE_TIMERS: { doNotFake: ('Date' | 'setImmediate' | 'nextTick')[] } = {
  doNotFake: ['Date', 'setImmediate', 'nextTick'],
};

beforeEach(async () => {
  __http.replyAll({ status: 202 });
  await signInTestUser();
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
  it('carries a trimmed message, stack and the app version', async () => {
    reportClientError(new Error('boom'), 'uncaught');

    await settle();
    expect(delivered()).toEqual([expect.objectContaining({ type: 'client_error' })]);
    const payload = lastPayload();
    expect(payload['source']).toBe('uncaught');
    expect(payload['message']).toBe('boom');
    expect(typeof payload['stack']).toBe('string');
    expect(payload['app_version']).toEqual(expect.any(String));
  });

  it('trims a message past the size cap', async () => {
    reportClientError(new Error('x'.repeat(1000)), 'boundary');

    await settle();
    const message = lastPayload()['message'] as string;
    expect(message.length).toBeLessThan(600);
    expect(message.endsWith('…')).toBe(true);
  });

  it('stringifies a non-Error thrown value, with no stack', async () => {
    reportClientError('a plain string throw', 'unhandled_rejection');

    await settle();
    const payload = lastPayload();
    expect(payload['message']).toBe('a plain string throw');
    expect('stack' in payload).toBe(false);
  });

  it('carries the configured app version', async () => {
    reportClientError(new Error('boom'), 'uncaught');

    await settle();
    expect(lastPayload()['app_version']).toBe('1.2.3');
  });

  it('falls back to "dev" when no app version is configured', async () => {
    mockExpoConfig = undefined;

    reportClientError(new Error('boom'), 'uncaught');

    await settle();
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

  it('enqueues one client_error for the same error repeated inside the window', async () => {
    const error = new Error('render loop');
    for (let i = 0; i < 5; i += 1) reportClientError(error, 'boundary');

    await settle();
    expect(delivered()).toHaveLength(1);
  });

  it('enqueues distinct errors and the same message from another source', async () => {
    reportClientError(new Error('first'), 'boundary');
    reportClientError(new Error('second'), 'boundary');
    reportClientError(new Error('first'), 'uncaught');

    await settle();
    expect(delivered()).toHaveLength(THREE_REPORTS);
  });

  it('reports again after the window, carrying the count of suppressed repeats', async () => {
    const error = new Error('render loop');
    for (let i = 0; i < 4; i += 1) reportClientError(error, 'boundary');
    now += WINDOW_MS;
    reportClientError(error, 'boundary');

    await settle();
    expect(delivered()).toHaveLength(TWO_REPORTS);
    expect(lastPayload()['suppressed_repeats']).toBe(3);
  });

  it('omits the count when nothing was suppressed', async () => {
    reportClientError(new Error('once'), 'boundary');

    await settle();
    expect('suppressed_repeats' in lastPayload()).toBe(false);
  });

  it('keeps reporting every distinct error past the tracking cap', async () => {
    for (let i = 0; i < DISTINCT_ERRORS; i += 1) {
      reportClientError(new Error(`distinct ${i}`), 'boundary');
      await settle();
    }

    await settle();
    expect(delivered()).toHaveLength(DISTINCT_ERRORS);
  });

  it('reports again when the clock is moved backwards', async () => {
    const error = new Error('render loop');
    reportClientError(error, 'boundary');
    now -= 3_600_000;
    reportClientError(error, 'boundary');

    await settle();
    expect(delivered()).toHaveLength(TWO_REPORTS);
  });

  it('delivers the count of a burst that stops inside the window when the window ends', async () => {
    jest.useFakeTimers(FAKE_TIMERS);
    const error = new Error('render loop');
    for (let i = 0; i < 4; i += 1) reportClientError(error, 'boundary');
    await forgetDelivered();

    now += WINDOW_MS;
    jest.advanceTimersByTime(WINDOW_MS);

    await settle();
    expect(delivered()).toHaveLength(1);
    expect(lastPayload()['suppressed_repeats']).toBe(3);
    jest.useRealTimers();
  });

  it('does not report a burst count twice', async () => {
    jest.useFakeTimers(FAKE_TIMERS);
    const error = new Error('render loop');
    for (let i = 0; i < 4; i += 1) reportClientError(error, 'boundary');
    now += WINDOW_MS;
    reportClientError(error, 'boundary');
    await forgetDelivered();

    jest.advanceTimersByTime(WINDOW_MS);

    await settle();
    expect(delivered()).toHaveLength(0);
    jest.useRealTimers();
  });

  it('keeps the hot repeating error suppressed while distinct errors fill the cap', async () => {
    const hot = new Error('hot');
    reportClientError(hot, 'boundary');
    reportClientError(hot, 'boundary');
    for (let i = 0; i < DISTINCT_ERRORS; i += 1) {
      reportClientError(new Error(`distinct ${i}`), 'boundary');
      await settle();
    }
    await forgetDelivered();

    reportClientError(hot, 'boundary');

    await settle();
    expect(delivered()).toHaveLength(0);
  });

  it('flushes the suppressed count of an expired error when a different error arrives', async () => {
    const stale = new Error('stale');
    reportClientError(stale, 'boundary');
    reportClientError(stale, 'boundary');
    reportClientError(stale, 'boundary');
    await forgetDelivered();

    now += WINDOW_MS;
    reportClientError(new Error('fresh'), 'boundary');

    await settle();
    expect(delivered()).toHaveLength(TWO_REPORTS);
    const [flushed, fresh] = delivered().map((event) => event.payload);
    expect(flushed).toMatchObject({ message: 'stale', suppressed_repeats: 2 });
    expect(fresh?.['message']).toBe('fresh');
    expect(fresh).not.toHaveProperty('suppressed_repeats');
  });

  it('when the table is full of repeating errors, evicts the oldest and flushes its count', async () => {
    for (let i = 0; i < 50; i += 1) {
      const error = new Error(`hot ${i}`);
      reportClientError(error, 'boundary');
      reportClientError(error, 'boundary');
      await settle();
    }
    await forgetDelivered();

    reportClientError(new Error('newcomer'), 'boundary');

    await settle();
    expect(delivered()).toHaveLength(TWO_REPORTS);
    expect(delivered()[0]?.payload).toMatchObject({
      message: 'hot 0',
      suppressed_repeats: 1,
    });
    await settle();
    expect(lastPayload()['message']).toBe('newcomer');
  });

  it('treats messages that differ only past the size cap as the same error', async () => {
    reportClientError(new Error(`${'x'.repeat(600)}a`), 'boundary');
    reportClientError(new Error(`${'x'.repeat(600)}b`), 'boundary');

    await settle();
    expect(delivered()).toHaveLength(1);
  });

  it('tells apart errors that differ only in their stack head', async () => {
    const first = new Error('same');
    first.stack = 'Error: same\n    at first (a.js:1:1)';
    const second = new Error('same');
    second.stack = 'Error: same\n    at second (b.js:2:2)';
    reportClientError(first, 'boundary');
    reportClientError(second, 'boundary');

    await settle();
    expect(delivered()).toHaveLength(TWO_REPORTS);
  });
});

describe('installGlobalErrorReporting', () => {
  it('wires the global handler and rejection tracking exactly once', () => {
    installGlobalErrorReporting();
    installGlobalErrorReporting();

    expect(mockSetGlobalHandler).toHaveBeenCalledTimes(1);
    expect(mockEnableRejectionTracking).toHaveBeenCalledTimes(1);
  });

  it('reports an uncaught error and forwards it to the previous handler', async () => {
    installGlobalErrorReporting();
    const installedHandler = mockSetGlobalHandler.mock.calls[0]?.[0] as (
      error: unknown,
      isFatal: boolean,
    ) => void;

    const error = new Error('native crash');
    installedHandler(error, true);

    await settle();
    expect(lastPayload()['source']).toBe('uncaught');
    expect(mockPreviousHandler).toHaveBeenCalledWith(error, true);
  });

  it('reports an unhandled rejection', async () => {
    installGlobalErrorReporting();
    const onUnhandled = mockEnableRejectionTracking.mock.calls[0]?.[0]?.onUnhandled as (
      id: number,
      error: unknown,
    ) => void;

    onUnhandled(1, new Error('dropped promise'));

    await settle();
    expect(lastPayload()['source']).toBe('unhandled_rejection');
  });

  it('reports nothing when a rejection is handled late', async () => {
    installGlobalErrorReporting();
    const onHandled = mockEnableRejectionTracking.mock.calls[0]?.[0]?.onHandled as (
      id: number,
    ) => void;

    onHandled(1);

    await settle();
    expect(delivered()).toHaveLength(0);
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
    __http.fail('GET /v1/discovery/search');

    const error = await apiFetch('/v1/discovery/search?q=secret').catch((e: unknown) => e);
    reportClientError(error, 'unhandled_rejection');

    await settle();
    expect(delivered()).toHaveLength(1);
    expect(JSON.stringify(lastPayload())).not.toContain('secret');
  });
});

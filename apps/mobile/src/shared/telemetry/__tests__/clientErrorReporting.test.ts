import { enqueueCritical } from '../outbox';
import {
  _resetGlobalErrorReportingForTest,
  installGlobalErrorReporting,
  reportClientError,
} from '../clientErrorReporting';

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

import { renderHook, act } from '@testing-library/react-native';
import * as Linking from 'expo-linking';
import { Platform } from 'react-native';

import { completeAuthIntent } from '../completeAuthIntent';
import { useAuthDeepLink } from '../hooks/useAuthDeepLink';

jest.mock('expo-router', () => ({ useRouter: () => ({ replace: jest.fn() }) }));
jest.mock('expo-linking', () => ({
  getInitialURL: jest.fn(),
  addEventListener: jest.fn(() => ({ remove: jest.fn() })),
}));
jest.mock('../completeAuthIntent', () => ({ completeAuthIntent: jest.fn() }));

const getInitialURL = Linking.getInitialURL as unknown as jest.Mock;
const mockComplete = completeAuthIntent as jest.Mock;

const flushMacrotask = (): Promise<void> => new Promise((resolve) => setImmediate(resolve));

describe('useAuthDeepLink: a rejected completeAuthIntent (#657)', () => {
  beforeEach(() => {
    getInitialURL.mockReset().mockResolvedValue(null);
    mockComplete.mockReset().mockResolvedValue({ kind: 'ignored' });
  });

  afterEach(() => {
    jest.restoreAllMocks();
  });

  it('swallows a rejected completeAuthIntent rather than leaking an unhandled rejection', async () => {
    jest.spyOn(console, 'warn').mockImplementation(() => undefined);
    const unhandled = jest.fn();
    process.on('unhandledRejection', unhandled);
    getInitialURL.mockResolvedValue('altune://auth/recovery?token_hash=x&type=recovery');
    mockComplete.mockRejectedValue(new Error('transport blew up'));

    renderHook(() => useAuthDeepLink());
    await act(async () => {
      await flushMacrotask();
    });
    await flushMacrotask();

    expect(mockComplete).toHaveBeenCalledTimes(1);
    expect(unhandled).not.toHaveBeenCalled();
    process.off('unhandledRejection', unhandled);
  });

  it('forwards a delivered auth link to completeAuthIntent', async () => {
    getInitialURL.mockResolvedValue('altune://auth/callback?code=abc');

    renderHook(() => useAuthDeepLink());
    await act(async () => {
      await flushMacrotask();
    });

    expect(mockComplete).toHaveBeenCalledTimes(1);
  });
});

const RECOVERY_LINK = 'altune://auth/recovery?token_hash=super-secret-hash&type=recovery';

/** Mounts the listener on the initial URL and settles the exchange it starts. */
async function deliverInitialLink(url: string): Promise<void> {
  getInitialURL.mockResolvedValue(url);
  renderHook(() => useAuthDeepLink());
  await act(async () => {
    await flushMacrotask();
  });
  await flushMacrotask();
}

describe('useAuthDeepLink: the trace a link that died in the background leaves (#1647)', () => {
  let warn: jest.SpyInstance;

  beforeEach(() => {
    getInitialURL.mockReset().mockResolvedValue(null);
    mockComplete.mockReset().mockResolvedValue({ kind: 'ignored' });
    warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
  });

  afterEach(() => {
    warn.mockRestore();
  });

  it('logs the intent kind and the cause when the exchange reports a failure', async () => {
    mockComplete.mockResolvedValue({
      kind: 'failure',
      cause: 'gotrue_rejected',
      error: { name: 'AuthApiError', code: 'otp_expired', status: 403 },
    });

    await deliverInitialLink(RECOVERY_LINK);

    expect(warn).toHaveBeenCalledWith('[auth] deep link exchange failed', {
      intent: 'recovery',
      cause: 'gotrue_rejected',
      error: { name: 'AuthApiError', code: 'otp_expired', status: 403 },
    });
  });

  it('logs the intent kind and the thrown error when the exchange rejects', async () => {
    mockComplete.mockRejectedValue(new Error('transport blew up'));

    await deliverInitialLink(RECOVERY_LINK);

    expect(warn).toHaveBeenCalledWith('[auth] deep link exchange threw', {
      intent: 'recovery',
      name: 'Error',
      message: 'transport blew up',
    });
  });

  it('logs nothing for a link that completed', async () => {
    mockComplete.mockResolvedValue({ kind: 'success' });

    await deliverInitialLink(RECOVERY_LINK);

    expect(warn).not.toHaveBeenCalled();
  });

  it('never writes the credential the link carried into the log', async () => {
    mockComplete.mockResolvedValue({ kind: 'failure', cause: 'no_spendable_credential' });

    await deliverInitialLink(RECOVERY_LINK);

    expect(warn).toHaveBeenCalledTimes(1);
    expect(JSON.stringify(warn.mock.calls)).not.toContain('super-secret-hash');
  });
});

// Regression for issue #2924: the web callback route (AuthCallbackScreen) owns
// completing the page URL. If this listener also read it via Linking's web
// shim (which mirrors window.location.href), the single-use code would be
// spent twice for one delivery.
describe('useAuthDeepLink: does not also complete the page URL on web (#2924)', () => {
  afterEach(() => {
    Platform.OS = 'ios';
  });

  it('never asks Linking for a URL to complete on web', async () => {
    Platform.OS = 'web';
    getInitialURL.mockReset().mockResolvedValue('https://app.altune.example/auth/callback?code=x');
    mockComplete.mockReset().mockResolvedValue({ kind: 'success' });

    renderHook(() => useAuthDeepLink());
    await act(async () => {
      await flushMacrotask();
    });

    expect(getInitialURL).not.toHaveBeenCalled();
    expect(mockComplete).not.toHaveBeenCalled();
  });
});

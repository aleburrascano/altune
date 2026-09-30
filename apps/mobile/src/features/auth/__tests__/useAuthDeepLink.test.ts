import { renderHook, act } from '@testing-library/react-native';
import * as Linking from 'expo-linking';
import { Platform } from 'react-native';

import { showAlert } from '@shared/ui/dialog/dialog';
import { completeAuthIntent } from '../completeAuthIntent';
import { useAuthDeepLink } from '../hooks/useAuthDeepLink';

jest.mock('expo-router', () => ({ useRouter: () => ({ replace: jest.fn() }) }));
jest.mock('expo-linking', () => ({
  getInitialURL: jest.fn(),
  addEventListener: jest.fn(() => ({ remove: jest.fn() })),
}));
jest.mock('@shared/ui/dialog/dialog', () => ({ showAlert: jest.fn() }));
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

describe('useAuthDeepLink: native completes a delivered url event (#2924 probe)', () => {
  const addEventListener = Linking.addEventListener as unknown as jest.Mock;

  beforeEach(() => {
    getInitialURL.mockReset().mockResolvedValue(null);
    mockComplete.mockReset().mockResolvedValue({ kind: 'success' });
    addEventListener.mockClear();
  });

  afterEach(() => {
    Platform.OS = 'ios';
  });

  it('completes an altune:// auth link delivered as a url event on native, once', async () => {
    Platform.OS = 'ios';

    renderHook(() => useAuthDeepLink());
    await act(async () => {
      await flushMacrotask();
    });
    expect(addEventListener).toHaveBeenCalledWith('url', expect.any(Function));
    const handler = addEventListener.mock.calls[0][1] as (event: { url: string }) => void;
    await act(async () => {
      handler({ url: 'altune://auth/callback?code=native-event' });
      await flushMacrotask();
    });

    expect(mockComplete).toHaveBeenCalledTimes(1);
  });
});

describe('useAuthDeepLink: a failed link is shown to the user (#686)', () => {
  const addEventListener = Linking.addEventListener as unknown as jest.Mock;
  const alert = showAlert as jest.Mock;

  beforeEach(() => {
    getInitialURL.mockReset().mockResolvedValue(null);
    mockComplete.mockReset().mockResolvedValue({ kind: 'success' });
    addEventListener.mockClear();
    alert.mockClear();
    jest.spyOn(console, 'warn').mockImplementation(() => undefined);
  });

  afterEach(() => {
    jest.restoreAllMocks();
  });

  async function deliverUrlEvent(url: string): Promise<void> {
    renderHook(() => useAuthDeepLink());
    await act(async () => {
      await flushMacrotask();
    });
    const handler = addEventListener.mock.calls[0][1] as (event: { url: string }) => void;
    await act(async () => {
      handler({ url });
      await flushMacrotask();
    });
  }

  it('alerts that an expired link did not work', async () => {
    mockComplete.mockResolvedValue({ kind: 'failure', cause: 'gotrue_rejected' });

    await deliverUrlEvent(RECOVERY_LINK);

    expect(alert).toHaveBeenCalledWith("This link didn't work", expect.stringContaining('expired'));
  });

  it('alerts to try again when the exchange throws', async () => {
    mockComplete.mockRejectedValue(new Error('timeout'));

    await deliverUrlEvent(RECOVERY_LINK);

    expect(alert).toHaveBeenCalledWith("Couldn't open that link", expect.stringContaining('again'));
  });

  it('does not alert for a completed link', async () => {
    await deliverUrlEvent(RECOVERY_LINK);

    expect(alert).not.toHaveBeenCalled();
  });

  it('catches a rejected getInitialURL without an unhandled rejection', async () => {
    const unhandled = jest.fn();
    process.on('unhandledRejection', unhandled);
    getInitialURL.mockRejectedValue(new Error('linking unavailable'));

    renderHook(() => useAuthDeepLink());
    await act(async () => {
      await flushMacrotask();
    });
    await flushMacrotask();

    expect(console.warn).toHaveBeenCalledWith('[auth] initial url read failed', {
      name: 'Error',
      message: 'linking unavailable',
    });
    expect(unhandled).not.toHaveBeenCalled();
    process.off('unhandledRejection', unhandled);
  });
});

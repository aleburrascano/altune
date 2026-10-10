import { renderHook, act } from '@testing-library/react-native';
import * as Linking from 'expo-linking';
import { Alert, Platform } from 'react-native';

import { _resetConsumedCredentialForTest } from '@features/auth/domain/completeAuthIntent';
import { useAuthDeepLink } from '@features/auth/hooks/useAuthDeepLink';

import { createSupabaseAuthMock } from '@features/auth/__tests__/testUtils/authTestUtils';

const mockRouter = { replace: jest.fn() };

jest.mock('expo-router', () => ({ useRouter: () => mockRouter }));
jest.mock('expo-linking', () => ({
  getInitialURL: jest.fn(),
  addEventListener: jest.fn(() => ({ remove: jest.fn() })),
}));
jest.mock('@shared/telemetry/userTelemetry', () => ({ recordFailureShown: jest.fn() }));
jest.mock('@shared/auth/supabaseClient', () => ({ supabase: { auth: {} } }));

const { exchangeCodeForSession, verifyOtp, getSession } = createSupabaseAuthMock(
  'exchangeCodeForSession',
  'verifyOtp',
  'getSession',
);

const getInitialURL = Linking.getInitialURL as unknown as jest.Mock;

const RECOVERING_USER_ID = 'user-recovering';
const EXPIRED_OTP = {
  name: 'AuthApiError',
  code: 'otp_expired',
  status: 403,
  message: 'Email link is invalid or has expired',
};

function verificationRefusedWith(error: unknown): void {
  verifyOtp.mockResolvedValue({ data: { user: null, session: null }, error });
}

beforeEach(() => {
  _resetConsumedCredentialForTest();
  mockRouter.replace.mockClear();
  getSession.mockResolvedValue({ data: { session: null }, error: null });
  verifyOtp.mockResolvedValue({
    data: { user: { id: RECOVERING_USER_ID }, session: null },
    error: null,
  });
  exchangeCodeForSession.mockResolvedValue({ data: { user: null, session: null }, error: null });
});

const flushMacrotask = (): Promise<void> => new Promise((resolve) => setImmediate(resolve));

describe('useAuthDeepLink: a rejected completeAuthIntent (#657)', () => {
  beforeEach(() => {
    getInitialURL.mockReset().mockResolvedValue(null);
  });

  afterEach(() => {
    jest.restoreAllMocks();
  });

  it('swallows a rejected link exchange rather than leaking an unhandled rejection', async () => {
    jest.spyOn(console, 'warn').mockImplementation(() => undefined);
    const unhandled = jest.fn();
    process.on('unhandledRejection', unhandled);
    getInitialURL.mockResolvedValue('altune://auth/recovery?token_hash=x&type=recovery');
    verifyOtp.mockRejectedValue(new Error('transport blew up'));

    renderHook(() => useAuthDeepLink());
    await act(async () => {
      await flushMacrotask();
    });
    await flushMacrotask();

    expect(verifyOtp).toHaveBeenCalledWith({ type: 'recovery', token_hash: 'x' });
    expect(unhandled).not.toHaveBeenCalled();
    process.off('unhandledRejection', unhandled);
  });

  it('spends the code a delivered auth link carries', async () => {
    getInitialURL.mockResolvedValue('altune://auth/callback?code=abc');

    renderHook(() => useAuthDeepLink());
    await act(async () => {
      await flushMacrotask();
    });

    expect(exchangeCodeForSession).toHaveBeenCalledWith('abc');
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
    warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
  });

  afterEach(() => {
    warn.mockRestore();
  });

  it('logs the intent kind and the cause when the exchange reports a failure', async () => {
    verificationRefusedWith(EXPIRED_OTP);

    await deliverInitialLink(RECOVERY_LINK);

    expect(warn).toHaveBeenCalledWith('[auth] deep link exchange failed', {
      intent: 'recovery',
      cause: 'gotrue_rejected',
      error: {
        name: 'AuthApiError',
        code: 'otp_expired',
        status: 403,
        message: 'Email link is invalid or has expired',
      },
    });
  });

  it('logs the intent kind and the thrown error when the exchange rejects', async () => {
    verifyOtp.mockRejectedValue(new Error('transport blew up'));

    await deliverInitialLink(RECOVERY_LINK);

    expect(warn).toHaveBeenCalledWith('[auth] deep link exchange threw', {
      intent: 'recovery',
      name: 'Error',
      message: 'transport blew up',
    });
  });

  it('logs nothing for a link that completed', async () => {
    await deliverInitialLink(RECOVERY_LINK);

    expect(mockRouter.replace).toHaveBeenCalledWith('/reset-password');
    expect(warn).not.toHaveBeenCalled();
  });

  it('never writes the credential the link carried into the log', async () => {
    verificationRefusedWith(EXPIRED_OTP);

    await deliverInitialLink(RECOVERY_LINK);

    expect(warn).toHaveBeenCalledTimes(1);
    expect(JSON.stringify(warn.mock.calls)).not.toContain('super-secret-hash');
  });
});

describe('useAuthDeepLink: native completes a delivered url event (#2924 probe)', () => {
  const addEventListener = Linking.addEventListener as unknown as jest.Mock;

  beforeEach(() => {
    getInitialURL.mockReset().mockResolvedValue(null);
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

    expect(exchangeCodeForSession).toHaveBeenCalledTimes(1);
    expect(exchangeCodeForSession).toHaveBeenCalledWith('native-event');
  });
});

describe('useAuthDeepLink: a failed link is shown to the user (#686)', () => {
  const addEventListener = Linking.addEventListener as unknown as jest.Mock;
  let alert: jest.SpyInstance;

  beforeEach(() => {
    getInitialURL.mockReset().mockResolvedValue(null);
    addEventListener.mockClear();
    alert = jest.spyOn(Alert, 'alert').mockImplementation(() => undefined);
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
    verificationRefusedWith(EXPIRED_OTP);

    await deliverUrlEvent(RECOVERY_LINK);

    expect(alert).toHaveBeenCalledWith("This link didn't work", expect.stringContaining('expired'));
  });

  it('alerts to try again when the exchange throws', async () => {
    verifyOtp.mockRejectedValue(new Error('timeout'));

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

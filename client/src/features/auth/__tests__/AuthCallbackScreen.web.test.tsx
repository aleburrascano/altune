import { act, render, screen, waitFor } from '@testing-library/react-native';
import { Platform } from 'react-native';

import { AUTH_ACTION_TIMEOUT_MS } from '@shared/auth/authDeadline';
import { _resetConsumedCredentialForTest } from '@features/auth/domain/completeAuthIntent';
import { AuthCallbackScreen } from '@features/auth/ui/AuthCallbackScreen.web';
import * as nativeWebOrigin from '@shared/device/webOrigin';
import * as browserWebOrigin from '@shared/device/webOrigin.web';

import { createSupabaseAuthMock } from '@features/auth/__tests__/testUtils/authTestUtils';

const mockRouter = { replace: jest.fn() };

jest.mock('expo-router', () => {
  const { View } = require('react-native');
  return {
    useRouter: () => mockRouter,
    Link: ({ children, testID }: { children: React.ReactNode; testID?: string }) => (
      <View testID={testID}>{children}</View>
    ),
  };
});

const { exchangeCodeForSession, verifyOtp, getSession } = createSupabaseAuthMock(
  'exchangeCodeForSession',
  'verifyOtp',
  'getSession',
);

const BAD_REQUEST = 400;
const FORBIDDEN = 403;

const SIGNED_IN = { data: { session: {}, user: { id: 'user-1' } }, error: null };

function rejectedBy(code: string, status: number) {
  return {
    data: { session: null, user: null },
    error: { name: 'AuthApiError', message: 'rejected', status, code },
  };
}

type ExchangeReply = typeof SIGNED_IN | ReturnType<typeof rejectedBy>;

function holdExchange(): (reply: ExchangeReply) => void {
  let release: ((reply: ExchangeReply) => void) | null = null;
  exchangeCodeForSession.mockImplementation(
    () => new Promise<ExchangeReply>((resolve) => (release = resolve)),
  );
  return (reply) => release?.(reply);
}

function setWebUrl(url: string): { replaceState: jest.Mock } {
  const parsed = new URL(url);
  const replaceState = jest.fn();
  Object.assign(globalThis, {
    window: {
      location: { href: url, pathname: parsed.pathname, origin: parsed.origin },
      history: { replaceState },
    },
  });
  return { replaceState };
}

beforeEach(() => {
  Platform.OS = 'web';
  jest.spyOn(nativeWebOrigin, 'webOrigin').mockImplementation(browserWebOrigin.webOrigin);
  mockRouter.replace.mockReset();
  _resetConsumedCredentialForTest();
  exchangeCodeForSession.mockResolvedValue(SIGNED_IN);
  verifyOtp.mockResolvedValue(SIGNED_IN);
  getSession.mockResolvedValue({ data: { session: null }, error: null });
});

afterEach(() => {
  jest.useRealTimers();
  jest.mocked(nativeWebOrigin.webOrigin).mockRestore();
  Platform.OS = 'ios';
  Reflect.deleteProperty(globalThis, 'window');
});

describe('AuthCallbackScreen: web auth completion (#2924)', () => {
  it('verifies a recovery link and lands on /reset-password', async () => {
    setWebUrl('https://app.altune.example/auth/recovery?token_hash=ok-1&type=recovery');

    render(<AuthCallbackScreen />);

    await waitFor(() => expect(mockRouter.replace).toHaveBeenCalledWith('/reset-password'));
    expect(verifyOtp).toHaveBeenCalledWith({ type: 'recovery', token_hash: 'ok-1' });
    expect(screen.queryByTestId('auth-callback-error')).toBeNull();
    expect(mockRouter.replace).toHaveBeenCalledTimes(1);
  });

  it('completes a confirm link and lands signed in on the library', async () => {
    setWebUrl('https://app.altune.example/auth/confirm?token_hash=ok-2&type=signup');

    render(<AuthCallbackScreen />);

    await waitFor(() => expect(mockRouter.replace).toHaveBeenCalledWith('/library'));
    expect(verifyOtp).toHaveBeenCalledWith({ type: 'signup', token_hash: 'ok-2' });
  });

  it('completes an OAuth callback and lands on the library', async () => {
    setWebUrl('https://app.altune.example/auth/callback?code=abc123');

    render(<AuthCallbackScreen />);

    await waitFor(() => expect(mockRouter.replace).toHaveBeenCalledWith('/library'));
    expect(exchangeCodeForSession).toHaveBeenCalledWith('abc123');
  });

  it('lands on the library for a second delivery of an already spent OAuth code, same as a fresh success', async () => {
    setWebUrl('https://app.altune.example/auth/callback?code=abc123');
    const first = render(<AuthCallbackScreen />);
    await waitFor(() => expect(mockRouter.replace).toHaveBeenCalledWith('/library'));
    first.unmount();
    mockRouter.replace.mockReset();
    setWebUrl('https://app.altune.example/auth/callback?code=abc123');

    render(<AuthCallbackScreen />);

    await waitFor(() => expect(mockRouter.replace).toHaveBeenCalledWith('/library'));
    expect(screen.queryByTestId('auth-callback-error')).toBeNull();
    expect(exchangeCodeForSession).toHaveBeenCalledTimes(1);
  });

  it('strips the code and tokens from window.location before completing', async () => {
    const { replaceState } = setWebUrl('https://app.altune.example/auth/callback?code=abc123');

    render(<AuthCallbackScreen />);

    await waitFor(() => expect(mockRouter.replace).toHaveBeenCalled());
    expect(replaceState).toHaveBeenCalledWith(null, '', '/auth/callback');
  });

  it('shows an error with a way back to sign-in for a failed exchange, and does not navigate to the library', async () => {
    jest.spyOn(console, 'warn').mockImplementation(() => undefined);
    setWebUrl('https://app.altune.example/auth/recovery?token_hash=expired&type=recovery');
    verifyOtp.mockResolvedValue(rejectedBy('otp_expired', FORBIDDEN));

    render(<AuthCallbackScreen />);

    await waitFor(() => expect(screen.getByTestId('auth-callback-error')).toBeTruthy());
    expect(mockRouter.replace).not.toHaveBeenCalled();
    expect(screen.getByTestId('auth-callback-error')).toHaveTextContent(/didn.t work/);
    expect(screen.getByTestId('auth-callback-error')).toHaveTextContent(/already been used/);
    expect(screen.getByTestId('auth-callback-signin')).toBeTruthy();
  });

  it('shows the error notice rather than an unhandled rejection when the exchange throws', async () => {
    jest.spyOn(console, 'warn').mockImplementation(() => undefined);
    setWebUrl('https://app.altune.example/auth/callback?code=bad');
    exchangeCodeForSession.mockRejectedValue(new Error('transport blew up'));

    render(<AuthCallbackScreen />);

    await waitFor(() => expect(screen.getByTestId('auth-callback-error')).toBeTruthy());
  });

  it('fails closed when there is no window to read a URL from', async () => {
    jest.spyOn(console, 'warn').mockImplementation(() => undefined);
    Reflect.deleteProperty(globalThis, 'window');

    render(<AuthCallbackScreen />);

    await waitFor(() => expect(screen.getByTestId('auth-callback-error')).toBeTruthy());
    expect(exchangeCodeForSession).not.toHaveBeenCalled();
    expect(verifyOtp).not.toHaveBeenCalled();
  });

  it('shows the pending state, not the error notice, while the exchange is still in flight', async () => {
    setWebUrl('https://app.altune.example/auth/callback?code=abc123');
    const release = holdExchange();

    render(<AuthCallbackScreen />);

    expect(screen.getByTestId('auth-callback-pending')).toHaveTextContent(/Signing you in/);
    expect(screen.queryByTestId('auth-callback-error')).toBeNull();
    await waitFor(() => expect(exchangeCodeForSession).toHaveBeenCalledWith('abc123'));

    release(SIGNED_IN);
    await waitFor(() => expect(mockRouter.replace).toHaveBeenCalledWith('/library'));
  });

  it('drops a resolution that arrives after unmount instead of updating unmounted state', async () => {
    jest.spyOn(console, 'warn').mockImplementation(() => undefined);
    setWebUrl('https://app.altune.example/auth/callback?code=abc123');
    const release = holdExchange();
    const errorSpy = jest.spyOn(console, 'error').mockImplementation(() => undefined);

    const { unmount } = render(<AuthCallbackScreen />);
    await waitFor(() => expect(exchangeCodeForSession).toHaveBeenCalled());
    unmount();
    release(rejectedBy('flow_state_expired', BAD_REQUEST));
    await Promise.resolve();
    await Promise.resolve();

    expect(errorSpy).not.toHaveBeenCalled();
    errorSpy.mockRestore();
  });
});

describe('AuthCallbackScreen: what a caller can hand the page beyond the happy path (#2924 probe)', () => {
  function openPage(url: string): jest.Mock {
    const parsed = new URL(url);
    const replaceState = jest.fn();
    Object.assign(globalThis, {
      window: {
        location: {
          href: url,
          pathname: parsed.pathname,
          origin: parsed.origin,
          search: parsed.search,
          hash: parsed.hash,
          host: parsed.host,
          protocol: parsed.protocol,
        },
        history: { replaceState },
      },
    });
    return replaceState;
  }

  function addressBarWrites(replaceState: jest.Mock): string {
    return replaceState.mock.calls.map((call) => String(call[2])).join(' ');
  }

  it('strips the OAuth code from the address bar before landing on the library', async () => {
    const replaceState = openPage('https://app.altune.example/auth/callback?code=code-secret-1');

    render(<AuthCallbackScreen />);

    await waitFor(() => expect(mockRouter.replace).toHaveBeenCalledWith('/library'));
    expect(exchangeCodeForSession).toHaveBeenCalledWith('code-secret-1');
    expect(replaceState).toHaveBeenCalled();
    expect(addressBarWrites(replaceState)).not.toContain('code-secret-1');
    expect(replaceState.mock.invocationCallOrder[0]).toBeLessThan(
      mockRouter.replace.mock.invocationCallOrder[0] ?? 0,
    );
  });

  it('strips the recovery token_hash from the address bar before landing on /reset-password', async () => {
    const replaceState = openPage(
      'https://app.altune.example/auth/recovery?token_hash=hash-secret-2&type=recovery',
    );

    render(<AuthCallbackScreen />);

    await waitFor(() => expect(mockRouter.replace).toHaveBeenCalledWith('/reset-password'));
    expect(addressBarWrites(replaceState)).not.toContain('hash-secret-2');
    expect(replaceState.mock.invocationCallOrder[0]).toBeLessThan(
      mockRouter.replace.mock.invocationCallOrder[0] ?? 0,
    );
  });

  it('strips the code from the address bar even when the exchange is rejected', async () => {
    const replaceState = openPage('https://app.altune.example/auth/callback?code=code-secret-3');
    exchangeCodeForSession.mockResolvedValue(rejectedBy('flow_state_expired', BAD_REQUEST));

    render(<AuthCallbackScreen />);

    await waitFor(() => expect(screen.getByTestId('auth-callback-error')).toBeTruthy());
    expect(replaceState).toHaveBeenCalled();
    expect(addressBarWrites(replaceState)).not.toContain('code-secret-3');
  });

  it('strips an access/refresh token pair carried in the hash fragment', async () => {
    const replaceState = openPage(
      'https://app.altune.example/auth/callback#access_token=at-secret-4&refresh_token=rt-secret-4&token_type=bearer',
    );

    render(<AuthCallbackScreen />);

    await waitFor(() => expect(screen.getByTestId('auth-callback-error')).toBeTruthy());
    expect(replaceState).toHaveBeenCalled();
    expect(addressBarWrites(replaceState)).not.toMatch(/at-secret-4|rt-secret-4/);
  });

  it('strips a token_hash carried in the hash fragment of a confirm link', async () => {
    const replaceState = openPage(
      'https://app.altune.example/auth/confirm?type=signup#token_hash=hash-secret-5',
    );

    render(<AuthCallbackScreen />);

    await waitFor(() => expect(replaceState).toHaveBeenCalled());
    expect(addressBarWrites(replaceState)).not.toContain('hash-secret-5');
  });

  it('shows the Supabase error redirect as a failure with the way back to sign-in, spending nothing', async () => {
    openPage(
      'https://app.altune.example/auth/confirm?error=access_denied&error_code=otp_expired&error_description=Email+link+is+invalid+or+has+expired',
    );

    render(<AuthCallbackScreen />);

    await waitFor(() => expect(screen.getByTestId('auth-callback-error')).toBeTruthy());
    expect(screen.getByTestId('auth-callback-signin')).toBeTruthy();
    expect(verifyOtp).not.toHaveBeenCalled();
    expect(exchangeCodeForSession).not.toHaveBeenCalled();
    expect(mockRouter.replace).not.toHaveBeenCalled();
  });

  it('shows the Supabase OAuth error redirect as a failure with the way back to sign-in', async () => {
    openPage(
      'https://app.altune.example/auth/callback?error=access_denied&error_description=User+denied+access#error=access_denied&error_description=User+denied+access',
    );

    render(<AuthCallbackScreen />);

    await waitFor(() => expect(screen.getByTestId('auth-callback-error')).toBeTruthy());
    expect(screen.getByTestId('auth-callback-signin')).toBeTruthy();
    expect(mockRouter.replace).not.toHaveBeenCalled();
  });

  it.each([
    ['/auth/callback'],
    ['/auth/confirm'],
    ['/auth/recovery'],
    ['/auth/callback?code='],
    ['/auth/recovery?token_hash=&type=recovery'],
  ])(
    'shows the error with the way back to sign-in for %s, which carries no credential',
    async (path) => {
      openPage(`https://app.altune.example${path}`);

      render(<AuthCallbackScreen />);

      await waitFor(() => expect(screen.getByTestId('auth-callback-error')).toBeTruthy());
      expect(screen.getByTestId('auth-callback-signin')).toBeTruthy();
      expect(exchangeCodeForSession).not.toHaveBeenCalled();
      expect(verifyOtp).not.toHaveBeenCalled();
      expect(mockRouter.replace).not.toHaveBeenCalled();
    },
  );

  it('settles out of the pending state on a code with broken percent-encoding', async () => {
    openPage('https://app.altune.example/auth/callback?code=%E0%A4%A');

    render(<AuthCallbackScreen />);

    await waitFor(() => expect(screen.queryByTestId('auth-callback-pending')).toBeNull());
    expect(mockRouter.replace).not.toHaveBeenCalledWith('/reset-password');
  });

  it('spends the code once and lands on the library under StrictMode double-mount', async () => {
    const { StrictMode } = require('react') as {
      StrictMode: React.ExoticComponent<{ children?: React.ReactNode }>;
    };
    openPage('https://app.altune.example/auth/callback?code=code-strict-6');

    render(
      <StrictMode>
        <AuthCallbackScreen />
      </StrictMode>,
    );

    await waitFor(() => expect(mockRouter.replace).toHaveBeenCalledWith('/library'));
    expect(exchangeCodeForSession).toHaveBeenCalledTimes(1);
    expect(screen.queryByTestId('auth-callback-error')).toBeNull();
  });

  it('spends the code once when the screen re-renders mid-exchange', async () => {
    openPage('https://app.altune.example/auth/callback?code=code-rerender-7');

    const { rerender } = render(<AuthCallbackScreen />);
    rerender(<AuthCallbackScreen />);
    rerender(<AuthCallbackScreen />);

    await waitFor(() => expect(mockRouter.replace).toHaveBeenCalledWith('/library'));
    expect(exchangeCodeForSession).toHaveBeenCalledTimes(1);
  });
});

describe('AuthCallbackScreen: web failures are reported with their cause', () => {
  const url = 'https://app.altune.example/auth/callback?code=code-secret-9';
  let warnSpy: jest.SpyInstance;

  beforeEach(() => {
    warnSpy = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
  });
  afterEach(() => warnSpy.mockRestore());

  it('warns with the cause of a refused exchange, without the url or code', async () => {
    setWebUrl(url);
    exchangeCodeForSession.mockResolvedValue(rejectedBy('flow_state_expired', BAD_REQUEST));

    render(<AuthCallbackScreen />);

    await waitFor(() => expect(screen.getByTestId('auth-callback-error')).toBeTruthy());
    expect(warnSpy).toHaveBeenCalledWith(
      expect.any(String),
      expect.objectContaining({ cause: 'gotrue_rejected' }),
    );
    expect(JSON.stringify(warnSpy.mock.calls)).not.toContain('code-secret-9');
  });

  it('warns and shows connection copy when the exchange times out', async () => {
    jest.useFakeTimers();
    setWebUrl(url);
    holdExchange();

    render(<AuthCallbackScreen />);
    await act(async () => {
      jest.advanceTimersByTime(AUTH_ACTION_TIMEOUT_MS);
    });
    jest.useRealTimers();

    await waitFor(() => expect(screen.getByTestId('auth-callback-error')).toBeTruthy());
    expect(warnSpy).toHaveBeenCalledTimes(1);
    expect(screen.getByTestId('auth-callback-error')).toHaveTextContent(/connection/);
    expect(screen.getByTestId('auth-callback-error')).not.toHaveTextContent(/expired/);
  });

  it('warns when there is no url to read', async () => {
    Reflect.deleteProperty(globalThis, 'window');

    render(<AuthCallbackScreen />);

    await waitFor(() => expect(screen.getByTestId('auth-callback-error')).toBeTruthy());
    expect(warnSpy).toHaveBeenCalledTimes(1);
  });
});

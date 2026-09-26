import { render, screen, waitFor } from '@testing-library/react-native';
import { Platform } from 'react-native';

import { type AuthIntentResult, completeAuthIntent } from '../completeAuthIntent';
import { AuthCallbackScreen } from '../ui/AuthCallbackScreen';

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
jest.mock('@shared/auth/supabaseClient', () => ({ supabase: { auth: {} } }));
jest.mock('../completeAuthIntent', () => ({ completeAuthIntent: jest.fn() }));

const mockComplete = completeAuthIntent as jest.Mock;

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
  mockRouter.replace.mockReset();
  mockComplete.mockReset();
});

afterEach(() => {
  Platform.OS = 'ios';
  Reflect.deleteProperty(globalThis, 'window');
});

describe('AuthCallbackScreen: web auth completion (#2924)', () => {
  it('completes a recovery link and leaves the /reset-password landing to completeAuthIntent', async () => {
    setWebUrl('https://app.altune.example/auth/recovery?token_hash=ok-1&type=recovery');
    mockComplete.mockImplementation(async (_intent, router) => {
      router.replace('/reset-password');
      return { kind: 'success' };
    });

    render(<AuthCallbackScreen />);

    await waitFor(() => expect(mockRouter.replace).toHaveBeenCalledWith('/reset-password'));
    expect(screen.queryByTestId('auth-callback-error')).toBeNull();
    expect(mockRouter.replace).toHaveBeenCalledTimes(1);
  });

  it('completes a confirm link and lands signed in on the library', async () => {
    setWebUrl('https://app.altune.example/auth/confirm?token_hash=ok-2&type=signup');
    mockComplete.mockResolvedValue({ kind: 'success' });

    render(<AuthCallbackScreen />);

    await waitFor(() => expect(mockRouter.replace).toHaveBeenCalledWith('/library'));
  });

  it('completes an OAuth callback and lands on the library', async () => {
    setWebUrl('https://app.altune.example/auth/callback?code=abc123');
    mockComplete.mockResolvedValue({ kind: 'success' });

    render(<AuthCallbackScreen />);

    await waitFor(() => expect(mockRouter.replace).toHaveBeenCalledWith('/library'));
  });

  it('lands on the library for a deduped OAuth exchange, same as a fresh success', async () => {
    setWebUrl('https://app.altune.example/auth/callback?code=abc123');
    mockComplete.mockResolvedValue({ kind: 'deduped' });

    render(<AuthCallbackScreen />);

    await waitFor(() => expect(mockRouter.replace).toHaveBeenCalledWith('/library'));
    expect(screen.queryByTestId('auth-callback-error')).toBeNull();
  });

  it('strips the code and tokens from window.location before completing', async () => {
    const { replaceState } = setWebUrl('https://app.altune.example/auth/callback?code=abc123');
    mockComplete.mockResolvedValue({ kind: 'success' });

    render(<AuthCallbackScreen />);

    await waitFor(() => expect(mockRouter.replace).toHaveBeenCalled());
    expect(replaceState).toHaveBeenCalledWith(null, '', '/auth/callback');
  });

  it('shows an error with a way back to sign-in for a failed exchange, and does not navigate to the library', async () => {
    setWebUrl('https://app.altune.example/auth/recovery?token_hash=expired&type=recovery');
    mockComplete.mockResolvedValue({
      kind: 'failure',
      cause: 'gotrue_rejected',
      error: { name: 'AuthApiError', code: 'otp_expired', status: 403 },
    });

    render(<AuthCallbackScreen />);

    await waitFor(() => expect(screen.getByTestId('auth-callback-error')).toBeTruthy());
    expect(mockRouter.replace).not.toHaveBeenCalled();
    expect(screen.getByTestId('auth-callback-error')).toHaveTextContent(/didn.t work/);
    expect(screen.getByTestId('auth-callback-error')).toHaveTextContent(
      /already been used/,
    );
    expect(screen.getByTestId('auth-callback-signin')).toBeTruthy();
  });

  it('shows the error notice rather than an unhandled rejection when completeAuthIntent throws', async () => {
    setWebUrl('https://app.altune.example/auth/callback?code=bad');
    mockComplete.mockRejectedValue(new Error('transport blew up'));

    render(<AuthCallbackScreen />);

    await waitFor(() => expect(screen.getByTestId('auth-callback-error')).toBeTruthy());
  });

  it('fails closed with the error notice when there is no page URL to read (no window)', async () => {
    Platform.OS = 'ios';
    Reflect.deleteProperty(globalThis, 'window');

    render(<AuthCallbackScreen />);

    await waitFor(() => expect(screen.getByTestId('auth-callback-error')).toBeTruthy());
    expect(mockComplete).not.toHaveBeenCalled();
  });

  it('never reads window.location as the page URL off the web platform, even if a window exists', async () => {
    const { replaceState } = setWebUrl('https://app.altune.example/auth/callback?code=abc123');
    Platform.OS = 'ios';

    render(<AuthCallbackScreen />);

    await waitFor(() => expect(screen.getByTestId('auth-callback-error')).toBeTruthy());
    expect(mockComplete).not.toHaveBeenCalled();
    expect(replaceState).not.toHaveBeenCalled();
  });

  it('fails closed on the web platform too when there is no window to read a URL from', async () => {
    Platform.OS = 'web';
    Reflect.deleteProperty(globalThis, 'window');

    render(<AuthCallbackScreen />);

    await waitFor(() => expect(screen.getByTestId('auth-callback-error')).toBeTruthy());
    expect(mockComplete).not.toHaveBeenCalled();
  });

  it('shows the pending state, not the error notice, while the exchange is still in flight', async () => {
    setWebUrl('https://app.altune.example/auth/callback?code=abc123');
    let resolveComplete: (outcome: AuthIntentResult) => void = () => {};
    mockComplete.mockImplementation(
      () => new Promise((resolve) => (resolveComplete = resolve)),
    );

    render(<AuthCallbackScreen />);

    expect(screen.getByTestId('auth-callback-pending')).toHaveTextContent(/Signing you in/);
    expect(screen.queryByTestId('auth-callback-error')).toBeNull();

    resolveComplete({ kind: 'success' });
    await waitFor(() => expect(mockRouter.replace).toHaveBeenCalledWith('/library'));
  });

  it('drops a resolution that arrives after unmount instead of updating unmounted state', async () => {
    setWebUrl('https://app.altune.example/auth/callback?code=abc123');
    let resolveComplete: (outcome: AuthIntentResult) => void = () => {};
    mockComplete.mockImplementation(
      () => new Promise((resolve) => (resolveComplete = resolve)),
    );
    const errorSpy = jest.spyOn(console, 'error').mockImplementation(() => undefined);

    const { unmount } = render(<AuthCallbackScreen />);
    unmount();
    resolveComplete({ kind: 'failure', cause: 'gotrue_rejected' });
    await Promise.resolve();
    await Promise.resolve();

    expect(errorSpy).not.toHaveBeenCalled();
    errorSpy.mockRestore();
  });
});

describe('AuthCallbackScreen: what a caller can hand the page beyond the happy path (#2924 probe)', () => {
  const actual = jest.requireActual<{
    completeAuthIntent: typeof completeAuthIntent;
    _resetConsumedCredentialForTest: () => void;
  }>('../completeAuthIntent');
  const auth = (
    require('@shared/auth/supabaseClient') as { supabase: { auth: Record<string, unknown> } }
  ).supabase.auth;

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

  beforeEach(() => {
    actual._resetConsumedCredentialForTest();
    mockComplete.mockImplementation(actual.completeAuthIntent);
    auth.exchangeCodeForSession = jest
      .fn()
      .mockResolvedValue({ data: { session: {}, user: { id: 'user-1' } }, error: null });
    auth.verifyOtp = jest
      .fn()
      .mockResolvedValue({ data: { session: {}, user: { id: 'user-1' } }, error: null });
  });

  afterEach(() => {
    Reflect.deleteProperty(auth, 'exchangeCodeForSession');
    Reflect.deleteProperty(auth, 'verifyOtp');
  });

  it('strips the OAuth code from the address bar before landing on the library', async () => {
    const replaceState = openPage('https://app.altune.example/auth/callback?code=code-secret-1');

    render(<AuthCallbackScreen />);

    await waitFor(() => expect(mockRouter.replace).toHaveBeenCalledWith('/library'));
    expect(auth.exchangeCodeForSession).toHaveBeenCalledWith('code-secret-1');
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
    auth.exchangeCodeForSession = jest.fn().mockResolvedValue({
      data: { session: null, user: null },
      error: {
        name: 'AuthApiError',
        message: 'invalid flow state',
        status: 400,
        code: 'flow_state_expired',
      },
    });

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
    expect(auth.verifyOtp).not.toHaveBeenCalled();
    expect(auth.exchangeCodeForSession).not.toHaveBeenCalled();
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
      expect(auth.exchangeCodeForSession).not.toHaveBeenCalled();
      expect(auth.verifyOtp).not.toHaveBeenCalled();
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
    expect(auth.exchangeCodeForSession).toHaveBeenCalledTimes(1);
    expect(screen.queryByTestId('auth-callback-error')).toBeNull();
  });

  it('spends the code once when the screen re-renders mid-exchange', async () => {
    openPage('https://app.altune.example/auth/callback?code=code-rerender-7');

    const { rerender } = render(<AuthCallbackScreen />);
    rerender(<AuthCallbackScreen />);
    rerender(<AuthCallbackScreen />);

    await waitFor(() => expect(mockRouter.replace).toHaveBeenCalledWith('/library'));
    expect(auth.exchangeCodeForSession).toHaveBeenCalledTimes(1);
  });
});

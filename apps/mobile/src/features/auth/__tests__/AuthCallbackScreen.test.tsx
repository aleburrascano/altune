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

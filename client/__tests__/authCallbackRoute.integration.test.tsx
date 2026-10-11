import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import * as Linking from 'expo-linking';
import { Stack } from 'expo-router';
import { Text } from 'react-native';
import type { ReactNode } from 'react';
import { act, renderRouter, screen } from 'expo-router/testing-library';

import { supabase } from '@shared/auth/supabaseClient';

import { AuthGate } from '../src/features/auth/ui/AuthGate';
import { useAuthDeepLink } from '../src/features/auth/hooks/useAuthDeepLink';
import { clearRecoveryUnlock } from '../src/features/auth/domain/passwordRecovery';
import IndexScreen from '../src/app/index';
import AuthCallbackRoute from '../src/app/auth/callback';
import AuthConfirmRoute from '../src/app/auth/confirm';
import AuthRecoveryRoute from '../src/app/auth/recovery';

jest.mock('decode-uri-component', () => ({
  __esModule: true,
  default: (s: string) => {
    try {
      return decodeURIComponent(s);
    } catch {
      return s;
    }
  },
}));

jest.mock('expo-linking', () => ({
  getInitialURL: jest.fn(),
  addEventListener: jest.fn(() => ({ remove: jest.fn() })),
}));

type AuthCallback = (event: string, session: unknown) => void;

const RN = require('react-native');
const NATIVE_OS = 'ios';

let client: QueryClient;
let authCallbacks: AuthCallback[];

beforeEach(() => {
  RN.Platform.OS = NATIVE_OS;
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  (Linking.getInitialURL as jest.Mock).mockReset().mockResolvedValue(null);
  (Linking.addEventListener as jest.Mock).mockReset().mockReturnValue({ remove: jest.fn() });
  authCallbacks = [];
  jest.spyOn(supabase.auth, 'onAuthStateChange').mockImplementation(((cb: AuthCallback) => {
    authCallbacks.push(cb);
    return { data: { subscription: { unsubscribe: jest.fn() } } };
  }) as never);
  jest.spyOn(supabase.auth, 'verifyOtp');
  jest.spyOn(supabase.auth, 'exchangeCodeForSession');
  act(() => clearRecoveryUnlock());
});

afterEach(() => {
  client.clear();
  act(() => clearRecoveryUnlock());
});

function wrapper({ children }: { children: ReactNode }) {
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

function AuthDeepLinkBridge() {
  useAuthDeepLink();
  return null;
}

function RootTestLayout() {
  return (
    <>
      <AuthDeepLinkBridge />
      <AuthGate>
        <Stack screenOptions={{ headerShown: false }}>
          <Stack.Screen name="index" />
          <Stack.Screen name="auth/callback" />
          <Stack.Screen name="auth/confirm" />
          <Stack.Screen name="auth/recovery" />
          <Stack.Screen name="reset-password" />
        </Stack>
      </AuthGate>
    </>
  );
}

const ROUTES = {
  _layout: RootTestLayout,
  index: IndexScreen,
  'auth/callback': AuthCallbackRoute,
  'auth/confirm': AuthConfirmRoute,
  'auth/recovery': AuthRecoveryRoute,
  'reset-password': () => <Text>reset-password-screen</Text>,
  '(auth)/sign-in': () => <Text>sign-in-screen</Text>,
  discover: () => <Text>discover-screen</Text>,
  library: () => <Text>library-screen</Text>,
};

function signedIn(): void {
  jest.spyOn(supabase.auth, 'getSession').mockResolvedValue({
    data: { session: { access_token: 'tok', user: { id: 'user-1' } } },
    error: null,
  } as never);
}

function signedOut(): void {
  jest.spyOn(supabase.auth, 'getSession').mockResolvedValue({
    data: { session: null },
    error: null,
  });
}

describe('native auth deep-link routes never show the web error screen (#2991)', () => {
  it('leaves /auth/callback and never renders the web error notice when signed in', async () => {
    signedIn();

    const router = renderRouter(ROUTES, { initialUrl: '/auth/callback', wrapper });
    await act(async () => {});
    await act(async () => {});

    expect(screen.queryByTestId('auth-callback-error')).toBeNull();
    expect(router.getPathname()).not.toMatch(/^\/auth\//);
  });

  it('leaves /auth/confirm and never renders the web error notice when signed out', async () => {
    signedOut();

    const router = renderRouter(ROUTES, { initialUrl: '/auth/confirm', wrapper });
    await act(async () => {});
    await act(async () => {});

    expect(screen.queryByTestId('auth-callback-error')).toBeNull();
    expect(router.getPathname()).not.toMatch(/^\/auth\//);
  });
});

async function settle(times = 4): Promise<void> {
  for (let i = 0; i < times; i += 1) {
    await act(async () => {});
  }
}

describe('the deep-link bridge survives AuthGate redirecting away mid-exchange (#2991)', () => {
  it('calls the exchange exactly once for a confirm link while signed out, even though the route redirects', async () => {
    signedOut();
    (Linking.getInitialURL as jest.Mock).mockResolvedValue(
      'altune://auth/confirm?token_hash=tok-1&type=signup',
    );
    jest.spyOn(supabase.auth, 'verifyOtp').mockResolvedValue({
      data: { user: { id: 'user-1' } },
      error: null,
    } as never);

    renderRouter(ROUTES, { initialUrl: '/auth/confirm', wrapper });
    await settle();

    expect(supabase.auth.verifyOtp).toHaveBeenCalledTimes(1);
  });

  it('completes the recovery exchange and ends on /reset-password, not clobbered by the / redirect', async () => {
    signedOut();
    (Linking.getInitialURL as jest.Mock).mockResolvedValue(
      'altune://auth/recovery?token_hash=tok-2&type=recovery',
    );
    jest.spyOn(supabase.auth, 'verifyOtp').mockImplementation((async () => {
      signedIn();
      const session = { access_token: 'tok', user: { id: 'user-1' } };
      for (const handler of authCallbacks) handler('SIGNED_IN', session);
      return { data: { user: { id: 'user-1' } }, error: null };
    }) as never);

    const router = renderRouter(ROUTES, { initialUrl: '/auth/recovery', wrapper });
    await settle();

    expect(supabase.auth.verifyOtp).toHaveBeenCalledTimes(1);
    expect(screen.queryByTestId('invalid-recovery-link')).toBeNull();
    expect(screen.getByText('reset-password-screen')).toBeTruthy();
    expect(router.getPathname()).toBe('/reset-password');
  });

  it('exchanges the OAuth code exactly once for an /auth/callback link', async () => {
    signedOut();
    (Linking.getInitialURL as jest.Mock).mockResolvedValue('altune://auth/callback?code=abc-123');
    jest.spyOn(supabase.auth, 'exchangeCodeForSession').mockResolvedValue({ error: null } as never);

    renderRouter(ROUTES, { initialUrl: '/auth/callback', wrapper });
    await settle();

    expect(supabase.auth.exchangeCodeForSession).toHaveBeenCalledTimes(1);
    expect(supabase.auth.exchangeCodeForSession).toHaveBeenCalledWith('abc-123');
  });

  it('still hands a warm url event to the exchange after AuthGate has redirected away', async () => {
    signedOut();
    let warmHandler: ((event: { url: string }) => void) | undefined;
    (Linking.addEventListener as jest.Mock).mockImplementation((_type, handler) => {
      warmHandler = handler;
      return { remove: jest.fn() };
    });
    jest.spyOn(supabase.auth, 'verifyOtp').mockResolvedValue({
      data: { user: { id: 'user-1' } },
      error: null,
    } as never);

    const router = renderRouter(ROUTES, { initialUrl: '/auth/confirm', wrapper });
    await settle(8);

    expect(router.getPathname()).toBe('/sign-in');
    act(() => warmHandler?.({ url: 'altune://auth/confirm?token_hash=tok-3&type=signup' }));
    await settle();

    expect(supabase.auth.verifyOtp).toHaveBeenCalledTimes(1);
  });
});

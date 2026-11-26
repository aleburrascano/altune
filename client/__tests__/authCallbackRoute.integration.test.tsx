import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { Stack } from 'expo-router';
import { Text } from 'react-native';
import { type ReactNode, useEffect } from 'react';
import { act, renderRouter, screen } from 'expo-router/testing-library';

import { installAuthClient } from '@shared/auth';
import { authClientOver } from '@testing/fakeAuthClient';

import { useNavigator } from '@shared/navigation';
import {
  AuthGate,
  clearRecoveryUnlock,
  type LinkEvents,
  startAppAuthDeepLinks,
} from '@features/signIn';
import IndexScreen from '@/app/index';
import AuthCallbackRoute from '@/app/auth/callback';
import AuthConfirmRoute from '@/app/auth/confirm';
import AuthRecoveryRoute from '@/app/auth/recovery';

type AuthCallback = (event: string, session: unknown) => void;
type Session = { access_token: string; user: { id: string } };
type Verification = { data: { user: { id: string } }; error: null };

const RN = require('react-native');
const NATIVE_OS = 'ios';
const SESSION: Session = { access_token: 'tok', user: { id: 'user-1' } };
const VERIFIED: Verification = { data: { user: { id: 'user-1' } }, error: null };
const SETTLE_TURNS = 4;
const WARM_SETTLE_TURNS = 8;

let client: QueryClient;
let authCallbacks: AuthCallback[];
let current: Session | null;
let initialLink: string | null;
let warmListeners: ((url: string) => void)[];
let verifyOtp: jest.Mock;
let exchangeCodeForSession: jest.Mock;

const links: LinkEvents = {
  initialUrl: async () => initialLink,
  subscribeUrl: (listener) => {
    warmListeners.push(listener);
    return () => {};
  },
};

function installFakeAuth(): void {
  installAuthClient(
    authClientOver({
      getSession: async () => ({ data: { session: current }, error: null }),
      onAuthStateChange: (cb: AuthCallback) => {
        authCallbacks.push(cb);
        return { data: { subscription: { unsubscribe: () => {} } } };
      },
      verifyOtp,
      exchangeCodeForSession,
    }),
  );
}

beforeEach(() => {
  RN.Platform.OS = NATIVE_OS;
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  authCallbacks = [];
  warmListeners = [];
  current = null;
  initialLink = null;
  verifyOtp = jest.fn().mockResolvedValue(VERIFIED);
  exchangeCodeForSession = jest.fn().mockResolvedValue({ error: null });
  installFakeAuth();
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
  const router = useNavigator();

  useEffect(() => startAppAuthDeepLinks(links, router), [router]);
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
  current = SESSION;
}

function signedOut(): void {
  current = null;
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

async function settle(times = SETTLE_TURNS): Promise<void> {
  for (let i = 0; i < times; i += 1) {
    await act(async () => {});
  }
}

describe('the deep-link bridge survives AuthGate redirecting away mid-exchange (#2991)', () => {
  it('calls the exchange exactly once for a confirm link while signed out, even though the route redirects', async () => {
    signedOut();
    initialLink = 'altune://auth/confirm?token_hash=tok-1&type=signup';

    renderRouter(ROUTES, { initialUrl: '/auth/confirm', wrapper });
    await settle();

    expect(verifyOtp).toHaveBeenCalledTimes(1);
  });

  it('completes the recovery exchange and ends on /reset-password, not clobbered by the / redirect', async () => {
    signedOut();
    initialLink = 'altune://auth/recovery?token_hash=tok-2&type=recovery';
    verifyOtp.mockImplementation(async () => {
      signedIn();
      for (const handler of authCallbacks) handler('SIGNED_IN', SESSION);
      return VERIFIED;
    });

    const router = renderRouter(ROUTES, { initialUrl: '/auth/recovery', wrapper });
    await settle();

    expect(verifyOtp).toHaveBeenCalledTimes(1);
    expect(screen.queryByTestId('invalid-recovery-link')).toBeNull();
    expect(screen.getByText('reset-password-screen')).toBeTruthy();
    expect(router.getPathname()).toBe('/reset-password');
  });

  it('exchanges the OAuth code exactly once for an /auth/callback link', async () => {
    signedOut();
    initialLink = 'altune://auth/callback?code=abc-123';

    renderRouter(ROUTES, { initialUrl: '/auth/callback', wrapper });
    await settle();

    expect(exchangeCodeForSession).toHaveBeenCalledTimes(1);
    expect(exchangeCodeForSession).toHaveBeenCalledWith('abc-123');
  });

  it('still hands a warm url event to the exchange after AuthGate has redirected away', async () => {
    signedOut();

    const router = renderRouter(ROUTES, { initialUrl: '/auth/confirm', wrapper });
    await settle(WARM_SETTLE_TURNS);

    expect(router.getPathname()).toBe('/sign-in');
    act(() => {
      warmListeners.forEach((listener) =>
        listener('altune://auth/confirm?token_hash=tok-3&type=signup'),
      );
    });
    await settle();

    expect(verifyOtp).toHaveBeenCalledTimes(1);
  });
});

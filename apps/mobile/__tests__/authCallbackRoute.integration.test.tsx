import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { Stack } from 'expo-router';
import { Text } from 'react-native';
import type { ReactNode } from 'react';
import { act, renderRouter, screen } from 'expo-router/testing-library';

import { AuthGate } from '../src/features/auth/ui/AuthGate';
import IndexScreen from '../src/app/index';
import AuthCallbackRoute from '../src/app/auth/callback';
import AuthConfirmRoute from '../src/app/auth/confirm';

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

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: {
    auth: {
      getSession: jest.fn(),
      onAuthStateChange: jest.fn(() => ({ data: { subscription: { unsubscribe: jest.fn() } } })),
    },
  },
}));

const { supabase } = require('@shared/auth/supabaseClient');
const RN = require('react-native');
const NATIVE_OS = 'ios';

let client: QueryClient;

beforeEach(() => {
  RN.Platform.OS = NATIVE_OS;
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
});

afterEach(() => {
  client.clear();
});

function wrapper({ children }: { children: ReactNode }) {
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

function RootTestLayout() {
  return (
    <AuthGate>
      <Stack screenOptions={{ headerShown: false }}>
        <Stack.Screen name="index" />
        <Stack.Screen name="auth/callback" />
        <Stack.Screen name="auth/confirm" />
      </Stack>
    </AuthGate>
  );
}

const ROUTES = {
  _layout: RootTestLayout,
  index: IndexScreen,
  'auth/callback': AuthCallbackRoute,
  'auth/confirm': AuthConfirmRoute,
  '(auth)/sign-in': () => <Text>sign-in-screen</Text>,
  discover: () => <Text>discover-screen</Text>,
};

function signedIn(): void {
  (supabase.auth.getSession as jest.Mock).mockResolvedValue({
    data: { session: { access_token: 'tok', user: { id: 'user-1' } } },
    error: null,
  });
}

function signedOut(): void {
  (supabase.auth.getSession as jest.Mock).mockResolvedValue({
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

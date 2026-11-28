import { screen, waitFor } from '@testing-library/react-native';
import { renderRouter } from 'expo-router/testing-library';
import { Platform, Text } from 'react-native';

import { AuthCallbackScreen } from '@features/signIn/view/AuthCallbackScreen';
import { createSupabaseAuthMock } from '@features/signIn/__tests__/authTestUtils';
const ROUTES = {
  index: () => <Text>home-screen</Text>,
  'auth/callback': AuthCallbackScreen,
};

const { exchangeCodeForSession, verifyOtp, getSession } = createSupabaseAuthMock(
  'exchangeCodeForSession',
  'verifyOtp',
  'getSession',
);

function expectNothingSpent(): void {
  expect(exchangeCodeForSession).not.toHaveBeenCalled();
  expect(verifyOtp).not.toHaveBeenCalled();
  expect(getSession).not.toHaveBeenCalled();
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

afterEach(() => {
  Platform.OS = 'ios';
  Reflect.deleteProperty(globalThis, 'window');
});

describe('AuthCallbackScreen: native redirects home without a page URL (#2924)', () => {
  it('redirects home instead of showing the error notice when there is no page URL to read (no window)', async () => {
    Platform.OS = 'ios';
    Reflect.deleteProperty(globalThis, 'window');

    const router = renderRouter(ROUTES, { initialUrl: '/auth/callback' });

    await waitFor(() => expect(router.getPathname()).toBe('/'));
    expect(screen.getByText('home-screen')).toBeTruthy();
    expect(screen.queryByTestId('auth-callback-error')).toBeNull();
    expectNothingSpent();
  });

  it('never reads window.location as the page URL off the web platform, redirecting home instead even if a window exists', async () => {
    const { replaceState } = setWebUrl('https://app.altune.example/auth/callback?code=abc123');
    Platform.OS = 'ios';

    const router = renderRouter(ROUTES, { initialUrl: '/auth/callback' });

    await waitFor(() => expect(router.getPathname()).toBe('/'));
    expect(screen.getByText('home-screen')).toBeTruthy();
    expect(screen.queryByTestId('auth-callback-error')).toBeNull();
    expectNothingSpent();
    expect(replaceState).not.toHaveBeenCalled();
  });
});

describe('AuthCallbackScreen: native never attempts the exchange (#2991)', () => {
  it('does not spend the code with Supabase on native, unlike on web', async () => {
    setWebUrl('https://app.altune.example/auth/callback?code=abc123');
    Platform.OS = 'ios';
    exchangeCodeForSession.mockResolvedValue({ data: { session: {} }, error: null });

    renderRouter(ROUTES, { initialUrl: '/auth/callback' });
    await Promise.resolve();

    expectNothingSpent();
  });
});

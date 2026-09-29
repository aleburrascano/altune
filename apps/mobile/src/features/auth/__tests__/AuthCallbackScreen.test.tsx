import { render, screen, waitFor } from '@testing-library/react-native';
import { Platform } from 'react-native';

import { completeAuthIntent } from '../completeAuthIntent';
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
jest.mock('expo-router', () => {
  const { View } = require('react-native');
  return {
    useRouter: () => mockRouter,
    Link: ({ children, testID }: { children: React.ReactNode; testID?: string }) => (
      <View testID={testID}>{children}</View>
    ),
    Redirect: ({ href }: { href: string }) => <View testID={`redirect-${href}`} />,
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
  mockRouter.replace.mockReset();
  mockComplete.mockReset();
});

afterEach(() => {
  Platform.OS = 'ios';
  Reflect.deleteProperty(globalThis, 'window');
});

describe('AuthCallbackScreen: native redirects home without a page URL (#2924)', () => {
  it('redirects home instead of showing the error notice when there is no page URL to read (no window)', async () => {
    Platform.OS = 'ios';
    Reflect.deleteProperty(globalThis, 'window');

    render(<AuthCallbackScreen />);

    await waitFor(() => expect(screen.getByTestId('redirect-/')).toBeTruthy());
    expect(screen.queryByTestId('auth-callback-error')).toBeNull();
    expect(mockComplete).not.toHaveBeenCalled();
  });

  it('never reads window.location as the page URL off the web platform, redirecting home instead even if a window exists', async () => {
    const { replaceState } = setWebUrl('https://app.altune.example/auth/callback?code=abc123');
    Platform.OS = 'ios';

    render(<AuthCallbackScreen />);

    await waitFor(() => expect(screen.getByTestId('redirect-/')).toBeTruthy());
    expect(screen.queryByTestId('auth-callback-error')).toBeNull();
    expect(mockComplete).not.toHaveBeenCalled();
    expect(replaceState).not.toHaveBeenCalled();
  });
});

describe('AuthCallbackScreen: native never attempts the exchange (#2991)', () => {
  it('does not call completeAuthIntent on native, unlike on web', async () => {
    setWebUrl('https://app.altune.example/auth/callback?code=abc123');
    Platform.OS = 'ios';
    mockComplete.mockResolvedValue({ kind: 'success' });

    render(<AuthCallbackScreen />);
    await Promise.resolve();

    expect(mockComplete).not.toHaveBeenCalled();
  });
});

import { renderHook, act } from '@testing-library/react-native';
import * as Linking from 'expo-linking';

import { useAuthDeepLink } from '@features/auth/hooks/useAuthDeepLink.web';

import { createSupabaseAuthMock } from '@features/auth/__tests__/testUtils/authTestUtils';

jest.mock('expo-router', () => ({ useRouter: () => ({ replace: jest.fn() }) }));
jest.mock('expo-linking', () => ({
  getInitialURL: jest.fn(),
  addEventListener: jest.fn(() => ({ remove: jest.fn() })),
}));
jest.mock('@shared/auth/supabaseClient', () => ({ supabase: { auth: {} } }));

const { exchangeCodeForSession, verifyOtp } = createSupabaseAuthMock(
  'exchangeCodeForSession',
  'verifyOtp',
);

const getInitialURL = Linking.getInitialURL as unknown as jest.Mock;
const addEventListener = Linking.addEventListener as unknown as jest.Mock;

function expectNoCredentialSpent(): void {
  expect(exchangeCodeForSession).not.toHaveBeenCalled();
  expect(verifyOtp).not.toHaveBeenCalled();
}

const flushMacrotask = (): Promise<void> => new Promise((resolve) => setImmediate(resolve));

describe('useAuthDeepLink on web leaves the page URL to the route (#2924)', () => {
  beforeEach(() => {
    getInitialURL.mockReset().mockResolvedValue('https://app.altune.example/auth/callback?code=x');
    addEventListener.mockClear();
  });

  it('never asks Linking for a URL to complete on web', async () => {
    renderHook(() => useAuthDeepLink());
    await act(async () => {
      await flushMacrotask();
    });

    expect(getInitialURL).not.toHaveBeenCalled();
    expectNoCredentialSpent();
  });

  it('never subscribes to url events on web', async () => {
    renderHook(() => useAuthDeepLink());
    await act(async () => {
      await flushMacrotask();
    });

    expect(addEventListener).not.toHaveBeenCalled();
    expectNoCredentialSpent();
  });

  it('does not complete an /auth/callback URL delivered as a url event on web', async () => {
    renderHook(() => useAuthDeepLink());
    await act(async () => {
      await flushMacrotask();
    });
    for (const [, handler] of addEventListener.mock.calls) {
      await act(async () => {
        handler({ url: 'https://app.altune.example/auth/callback?code=web-event' });
        await flushMacrotask();
      });
    }

    expect(addEventListener).not.toHaveBeenCalled();
    expectNoCredentialSpent();
  });
});

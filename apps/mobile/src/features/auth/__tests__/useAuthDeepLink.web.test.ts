import { renderHook, act } from '@testing-library/react-native';
import * as Linking from 'expo-linking';

import { completeAuthIntent } from '../completeAuthIntent';
import { useAuthDeepLink } from '../hooks/useAuthDeepLink.web';

jest.mock('expo-router', () => ({ useRouter: () => ({ replace: jest.fn() }) }));
jest.mock('expo-linking', () => ({
  getInitialURL: jest.fn(),
  addEventListener: jest.fn(() => ({ remove: jest.fn() })),
}));
jest.mock('@shared/auth/supabaseClient', () => ({ supabase: { auth: {} } }));
jest.mock('../completeAuthIntent', () => ({ completeAuthIntent: jest.fn() }));

const getInitialURL = Linking.getInitialURL as unknown as jest.Mock;
const addEventListener = Linking.addEventListener as unknown as jest.Mock;
const mockComplete = completeAuthIntent as jest.Mock;

const flushMacrotask = (): Promise<void> => new Promise((resolve) => setImmediate(resolve));

describe('useAuthDeepLink on web leaves the page URL to the route (#2924)', () => {
  beforeEach(() => {
    getInitialURL.mockReset().mockResolvedValue('https://app.altune.example/auth/callback?code=x');
    addEventListener.mockClear();
    mockComplete.mockReset().mockResolvedValue({ kind: 'success' });
  });

  it('never asks Linking for a URL to complete on web', async () => {
    renderHook(() => useAuthDeepLink());
    await act(async () => {
      await flushMacrotask();
    });

    expect(getInitialURL).not.toHaveBeenCalled();
    expect(mockComplete).not.toHaveBeenCalled();
  });

  it('never subscribes to url events on web', async () => {
    renderHook(() => useAuthDeepLink());
    await act(async () => {
      await flushMacrotask();
    });

    expect(addEventListener).not.toHaveBeenCalled();
    expect(mockComplete).not.toHaveBeenCalled();
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
    expect(mockComplete).not.toHaveBeenCalled();
  });
});

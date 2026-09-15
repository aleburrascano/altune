import { renderHook, act } from '@testing-library/react-native';
import * as Linking from 'expo-linking';

import { completeAuthIntent } from '../completeAuthIntent';
import { useAuthDeepLink } from '../hooks/useAuthDeepLink';

jest.mock('expo-router', () => ({ useRouter: () => ({ replace: jest.fn() }) }));
jest.mock('expo-linking', () => ({
  getInitialURL: jest.fn(),
  addEventListener: jest.fn(() => ({ remove: jest.fn() })),
}));
jest.mock('../completeAuthIntent', () => ({ completeAuthIntent: jest.fn() }));

const getInitialURL = Linking.getInitialURL as unknown as jest.Mock;
const mockComplete = completeAuthIntent as jest.Mock;

const flushMacrotask = (): Promise<void> => new Promise((resolve) => setImmediate(resolve));

describe('useAuthDeepLink: a rejected completeAuthIntent (#657)', () => {
  beforeEach(() => {
    getInitialURL.mockReset().mockResolvedValue(null);
    mockComplete.mockReset().mockResolvedValue({ kind: 'ignored' });
  });

  it('swallows a rejected completeAuthIntent rather than leaking an unhandled rejection', async () => {
    const unhandled = jest.fn();
    process.on('unhandledRejection', unhandled);
    getInitialURL.mockResolvedValue('altune://auth/recovery?token_hash=x&type=recovery');
    mockComplete.mockRejectedValue(new Error('transport blew up'));

    renderHook(() => useAuthDeepLink());
    await act(async () => {
      await flushMacrotask();
    });
    await flushMacrotask();

    expect(mockComplete).toHaveBeenCalledTimes(1);
    expect(unhandled).not.toHaveBeenCalled();
    process.off('unhandledRejection', unhandled);
  });

  it('forwards a delivered auth link to completeAuthIntent', async () => {
    getInitialURL.mockResolvedValue('altune://auth/callback?code=abc');

    renderHook(() => useAuthDeepLink());
    await act(async () => {
      await flushMacrotask();
    });

    expect(mockComplete).toHaveBeenCalledTimes(1);
  });
});

/**
 * useSignOut — verifies the SDK call and the React Query cache clear
 * (Slice 14a, AC#5(a) + (c)).
 */
/* eslint-disable @typescript-eslint/no-require-imports */
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook } from '@testing-library/react-native';
import type { ReactNode } from 'react';

const mockSdkSignOut = jest.fn();
jest.mock('../supabaseClient', () => ({
  supabase: {
    auth: { signOut: () => mockSdkSignOut() },
  },
}));

function wrapper(queryClient: QueryClient) {
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  );
}

beforeEach(() => {
  mockSdkSignOut.mockReset();
});

describe('useSignOut', () => {
  it('calls supabase signOut and clears the react-query cache', async () => {
    mockSdkSignOut.mockResolvedValueOnce({ error: null });
    const queryClient = new QueryClient();
    queryClient.setQueryData(['library', 'page', 0], { items: ['a', 'b'] });

    const { useSignOut } = require('../useSignOut');
    const { result } = renderHook(() => useSignOut(), { wrapper: wrapper(queryClient) });

    await act(async () => {
      await result.current.signOut();
    });

    expect(mockSdkSignOut).toHaveBeenCalledTimes(1);
    expect(queryClient.getQueryData(['library', 'page', 0])).toBeUndefined();
    expect(result.current.state.kind).toBe('ok');
  });

  it('clears the cache and reports error when the sdk fails', async () => {
    mockSdkSignOut.mockResolvedValueOnce({ error: { message: 'network down' } });
    const queryClient = new QueryClient();
    queryClient.setQueryData(['anything'], 'stale');

    const { useSignOut } = require('../useSignOut');
    const { result } = renderHook(() => useSignOut(), { wrapper: wrapper(queryClient) });

    await act(async () => {
      await result.current.signOut();
    });

    expect(queryClient.getQueryData(['anything'])).toBeUndefined();
    expect(result.current.state.kind).toBe('error');
  });
});

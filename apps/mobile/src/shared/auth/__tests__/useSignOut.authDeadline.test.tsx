import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { renderHook, act } from '@testing-library/react-native';

import { NetworkError } from '@shared/errors';
import { onSignOut } from '@shared/session/signOutCleanup';

import { useSignOut } from '../useSignOut';
import { supabase } from '../supabaseClient';
import { clearPersistedAuthSession } from '../supabaseClient';

jest.mock('../supabaseClient', () => ({
  supabase: { auth: { signOut: jest.fn() } },
  clearPersistedAuthSession: jest.fn().mockResolvedValue(undefined),
}));

const mockSignOut = supabase.auth.signOut as jest.Mock;
const mockClearPersistedAuthSession = clearPersistedAuthSession as jest.Mock;

function createWrapper(queryClient: QueryClient) {
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
  };
}

let warn: jest.SpyInstance;

beforeEach(() => {
  jest.useFakeTimers();
  mockSignOut.mockReset();
  mockSignOut.mockReturnValue(new Promise(() => undefined));
  warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
});

afterEach(() => {
  warn.mockRestore();
  jest.useRealTimers();
});

describe('useSignOut() when the auth server never answers', () => {
  it('settles as a NetworkError(timeout) at 15000ms and still clears the previous user local data', async () => {
    const cleanup = jest.fn();
    const unregister = onSignOut(cleanup);
    const queryClient = new QueryClient();
    queryClient.setQueryData(['library', 'tracks'], ['cached-track']);
    const { result } = renderHook(() => useSignOut(), { wrapper: createWrapper(queryClient) });

    let signOutCall!: Promise<void>;
    act(() => {
      signOutCall = result.current.signOut();
    });

    await act(async () => {
      jest.advanceTimersByTime(14_999);
    });
    expect(result.current.state).toEqual({ status: 'loading' });
    expect(cleanup).not.toHaveBeenCalled();

    await act(async () => {
      jest.advanceTimersByTime(1);
      await signOutCall;
    });

    const { state } = result.current;
    expect(state.status).toBe('error');
    expect(state.status === 'error' && state.error).toBeInstanceOf(NetworkError);
    expect(state).toMatchObject({ error: { failure: 'timeout' } });
    expect(queryClient.getQueryData(['library', 'tracks'])).toBeUndefined();
    expect(cleanup).toHaveBeenCalledTimes(1);
    expect(mockClearPersistedAuthSession).toHaveBeenCalled();
    expect(warn).toHaveBeenCalledWith('[auth] sign out failed', { failure: 'timeout' });
    unregister();
  });
});

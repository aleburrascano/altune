import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { renderHook, act } from '@testing-library/react-native';

import { useSignOut } from '../useSignOut';
import { supabase } from '../supabaseClient';

jest.mock('../supabaseClient', () => ({
  supabase: { auth: { signOut: jest.fn() } },
}));

const mockSignOut = supabase.auth.signOut as jest.Mock;

function createWrapper(queryClient: QueryClient) {
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
  };
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((res) => {
    resolve = res;
  });
  return { promise, resolve };
}

beforeEach(() => {
  mockSignOut.mockReset();
});

describe('useSignOut(): the error ? … : … branch on the settled signOut() result', () => {
  it.each([
    ['error: null', 'ok', () => mockSignOut.mockResolvedValue({ error: null })],
    [
      'a non-null error object (offline / API blip)',
      'error',
      () => mockSignOut.mockResolvedValue({ error: { message: 'invalid_grant', status: 400 } }),
    ],
    [
      'a thrown/rejected signOut() call',
      'error',
      () => mockSignOut.mockRejectedValue(new Error('network request failed')),
    ],
  ] as const)('%s -> state.kind becomes %s, and the query cache is cleared regardless', async (_label, expectedKind, arrange) => {
    arrange();
    const queryClient = new QueryClient();
    queryClient.setQueryData(['library', 'tracks'], ['cached-track']);

    const { result } = renderHook(() => useSignOut(), { wrapper: createWrapper(queryClient) });

    await act(async () => {
      await result.current.signOut();
    });

    expect(result.current.state).toEqual({ kind: expectedKind });
    expect(queryClient.getQueryData(['library', 'tracks'])).toBeUndefined();
  });
});

describe('useSignOut(): idle -> pending -> ok, with pending actually observable mid-flight', () => {
  it('reports idle before signOut() is called, pending synchronously once invoked, then ok once it resolves', async () => {
    const gate = deferred<{ error: null }>();
    mockSignOut.mockReturnValueOnce(gate.promise);
    const queryClient = new QueryClient();
    const { result } = renderHook(() => useSignOut(), { wrapper: createWrapper(queryClient) });

    expect(result.current.state).toEqual({ kind: 'idle' });

    let signOutCall!: Promise<void>;
    act(() => {
      signOutCall = result.current.signOut();
    });

    expect(result.current.state).toEqual({ kind: 'pending' });

    await act(async () => {
      gate.resolve({ error: null });
      await signOutCall;
    });

    expect(result.current.state).toEqual({ kind: 'ok' });
  });
});

describe('useSignOut(): cache invalidation is exact, not merely spied on', () => {
  it('drops every distinct cached entry on a successful sign-out', async () => {
    mockSignOut.mockResolvedValue({ error: null });
    const queryClient = new QueryClient();
    queryClient.setQueryData(['library', 'tracks'], [{ id: 'track-1' }]);
    queryClient.setQueryData(['playlists', 'detail', 'playlist-1'], { id: 'playlist-1' });
    queryClient.setQueryData(['session'], { userId: 'user-1' });

    const { result } = renderHook(() => useSignOut(), { wrapper: createWrapper(queryClient) });

    await act(async () => {
      await result.current.signOut();
    });

    expect(queryClient.getQueryData(['library', 'tracks'])).toBeUndefined();
    expect(queryClient.getQueryData(['playlists', 'detail', 'playlist-1'])).toBeUndefined();
    expect(queryClient.getQueryData(['session'])).toBeUndefined();
    expect(queryClient.getQueryCache().getAll()).toHaveLength(0);
  });

  it('drops every distinct cached entry even when the sign-out request fails', async () => {
    mockSignOut.mockRejectedValue(new Error('network down'));
    const queryClient = new QueryClient();
    queryClient.setQueryData(['library', 'tracks'], [{ id: 'track-1' }]);
    queryClient.setQueryData(['session'], { userId: 'user-1' });

    const { result } = renderHook(() => useSignOut(), { wrapper: createWrapper(queryClient) });

    await act(async () => {
      await result.current.signOut();
    });

    expect(queryClient.getQueryCache().getAll()).toHaveLength(0);
  });
});

describe('useSignOut(): sequential replay', () => {
  it('signing out twice in a row is safe and lands in the same coherent terminal state both times', async () => {
    mockSignOut.mockResolvedValue({ error: null });
    const queryClient = new QueryClient();
    const { result } = renderHook(() => useSignOut(), { wrapper: createWrapper(queryClient) });

    await act(async () => {
      await result.current.signOut();
    });
    expect(result.current.state).toEqual({ kind: 'ok' });

    await act(async () => {
      await result.current.signOut();
    });
    expect(result.current.state).toEqual({ kind: 'ok' });
    expect(mockSignOut).toHaveBeenCalledTimes(2);
  });
});

describe('useSignOut(): ordering — the cache clear must not race the SDK call', () => {
  it('clears an entry written mid-flight, after signOut() has already settled — a refetch that lands on the still-valid session must not survive', async () => {
    const gate = deferred<{ error: null }>();
    mockSignOut.mockReturnValueOnce(gate.promise);
    const queryClient = new QueryClient();

    const { result } = renderHook(() => useSignOut(), { wrapper: createWrapper(queryClient) });

    let signOutCall!: Promise<void>;
    act(() => {
      signOutCall = result.current.signOut();
    });

    act(() => {
      queryClient.setQueryData(['library', 'tracks'], ['still-valid-session-data']);
    });
    expect(queryClient.getQueryData(['library', 'tracks'])).toEqual(['still-valid-session-data']);

    await act(async () => {
      gate.resolve({ error: null });
      await signOutCall;
    });

    expect(queryClient.getQueryData(['library', 'tracks'])).toBeUndefined();
  });
});

describe('useSignOut(): re-entry is gated by the caller, so pending must be observable before the first await', () => {
  it('exposes pending synchronously on the same tick signOut() is invoked, which is what every caller disables its control on', () => {
    const gate = deferred<{ error: null }>();
    mockSignOut.mockReturnValueOnce(gate.promise);
    const queryClient = new QueryClient();
    const { result } = renderHook(() => useSignOut(), { wrapper: createWrapper(queryClient) });

    let firstCall!: Promise<void>;
    act(() => {
      firstCall = result.current.signOut();
    });

    expect(result.current.state).toEqual({ kind: 'pending' });
    gate.resolve({ error: null });
    return act(async () => {
      await firstCall;
    });
  });
});

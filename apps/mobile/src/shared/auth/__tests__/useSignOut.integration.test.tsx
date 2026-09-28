import React from 'react';
import { QueryClient, QueryClientProvider, useQuery } from '@tanstack/react-query';
import { renderHook, act, waitFor } from '@testing-library/react-native';
import * as SecureStore from 'expo-secure-store';

import { apiFetch } from '@shared/api-client';
import { supabase } from '@shared/auth/supabaseClient';
import { useSignOut } from '../useSignOut';

const { __http } = require('../../../../jest/doubles/fetch.js');

function makeWrapper(queryClient: QueryClient) {
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
  };
}

describe('sign-out invalidates the React Query cache, verified by an authenticated query firing a fresh network fetch', () => {
  function useAuthenticatedLibraryQuery() {
    return useQuery({
      queryKey: ['library', 'tracks'],
      queryFn: () => apiFetch('/v1/library/tracks'),
    });
  }

  function stubAuthenticatedThenSignedOut(): void {
    jest.spyOn(supabase.auth, 'getSession').mockResolvedValue({
      data: { session: { access_token: 'user-a-token' } as never },
      error: null,
    });
    jest.spyOn(supabase.auth, 'signOut').mockResolvedValue({ error: null });
  }

  beforeEach(() => {
    __http.reset();
    jest.restoreAllMocks();
  });

  it('a representative authenticated hook (a library-tracks query), remounted after sign-out, re-fetches over the network instead of serving what was cached for the previous user', async () => {
    stubAuthenticatedThenSignedOut();
    __http.reply('GET /v1/library/tracks', {
      status: 200,
      json: [{ id: 'track-owned-by-user-a' }],
    });

    const queryClient = new QueryClient();
    const wrapper = makeWrapper(queryClient);

    const firstMount = renderHook(() => useAuthenticatedLibraryQuery(), { wrapper });
    await waitFor(() => expect(firstMount.result.current.isSuccess).toBe(true));
    expect(__http.countFor('GET /v1/library/tracks')).toBe(1);
    firstMount.unmount();

    const signOut = renderHook(() => useSignOut(), { wrapper });
    await act(async () => {
      await signOut.result.current.signOut();
    });
    signOut.unmount();

    const secondMount = renderHook(() => useAuthenticatedLibraryQuery(), { wrapper });
    await waitFor(() => expect(secondMount.result.current.isSuccess).toBe(true));

    expect(__http.countFor('GET /v1/library/tracks')).toBe(2);
    secondMount.unmount();
  });

  it('the cache is fully cleared, not merely marked stale — an infinite staleTime would otherwise let the remounted query reuse the previous user’s cached data with no fetch at all', async () => {
    stubAuthenticatedThenSignedOut();
    __http.reply('GET /v1/library/tracks', { status: 200, json: [{ id: 'track-1' }] });

    const queryClient = new QueryClient({
      defaultOptions: { queries: { staleTime: Infinity } },
    });
    const wrapper = makeWrapper(queryClient);

    const firstMount = renderHook(() => useAuthenticatedLibraryQuery(), { wrapper });
    await waitFor(() => expect(firstMount.result.current.isSuccess).toBe(true));
    expect(__http.countFor('GET /v1/library/tracks')).toBe(1);
    firstMount.unmount();

    const signOut = renderHook(() => useSignOut(), { wrapper });
    await act(async () => {
      await signOut.result.current.signOut();
    });
    signOut.unmount();

    const secondMount = renderHook(() => useAuthenticatedLibraryQuery(), { wrapper });
    await waitFor(() => expect(secondMount.result.current.isSuccess).toBe(true));

    expect(__http.countFor('GET /v1/library/tracks')).toBe(2);
    secondMount.unmount();
  });
});

describe('sign-out with the real auth client when the remote /logout cannot be reached', () => {
  const LOGOUT = 'POST /auth/v1/logout';

  async function storeSession(): Promise<void> {
    const session = {
      access_token: 'user-a-token',
      refresh_token: 'user-a-refresh',
      token_type: 'bearer',
      expires_in: 3600,
      expires_at: Math.floor(Date.now() / 1000) + 3600,
      user: { id: 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa', aud: 'authenticated' },
    };
    await SecureStore.setItemAsync('sb-fixture-auth-token', JSON.stringify(session));
  }

  beforeEach(() => {
    __http.reset();
    jest.restoreAllMocks();
    jest.spyOn(console, 'warn').mockImplementation(() => undefined);
  });

  it('emits SIGNED_OUT, leaves no session behind, and calls /logout exactly once', async () => {
    await storeSession();
    __http.fail(LOGOUT, new TypeError('Network request failed'));
    const events: string[] = [];
    const { data } = supabase.auth.onAuthStateChange((event) => {
      events.push(event);
    });
    const { result } = renderHook(() => useSignOut(), {
      wrapper: makeWrapper(new QueryClient()),
    });

    await act(async () => {
      await result.current.signOut();
    });
    data.subscription.unsubscribe();

    expect(events).toContain('SIGNED_OUT');
    expect((await supabase.auth.getSession()).data.session).toBeNull();
    expect(__http.countFor(LOGOUT)).toBe(1);
    expect(result.current.state.status).toBe('error');
  });
});

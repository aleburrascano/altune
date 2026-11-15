import React from 'react';
import { QueryClient, QueryClientProvider, useQuery } from '@tanstack/react-query';
import { renderHook, act, waitFor } from '@testing-library/react-native';
import * as SecureStore from 'expo-secure-store';

import { apiFetch } from '@shared/apiClient';
import { useSignOut } from '@shared/auth';
const { __http } = require('@testing/doubles/fetch.js');

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

  const USER_IDS = {
    a: 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa',
    b: 'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb',
  };

  async function storeSessionFor(user: 'a' | 'b'): Promise<void> {
    const session = {
      access_token: `user-${user}-token`,
      refresh_token: `user-${user}-refresh`,
      token_type: 'bearer',
      expires_in: 3600,
      expires_at: Math.floor(Date.now() / 1000) + 3600,
      user: { id: USER_IDS[user], aud: 'authenticated' },
    };
    await SecureStore.setItemAsync('sb-fixture-auth-token', JSON.stringify(session));
  }

  beforeEach(() => {
    __http.reset();
  });

  it('a representative authenticated hook (a library-tracks query), remounted after sign-out, re-fetches over the network instead of serving what was cached for the previous user', async () => {
    await storeSessionFor('a');
    __http.reply('POST /auth/v1/logout', { status: 204 });
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
    await storeSessionFor('b');

    const secondMount = renderHook(() => useAuthenticatedLibraryQuery(), { wrapper });
    await waitFor(() => expect(secondMount.result.current.isSuccess).toBe(true));

    expect(__http.countFor('GET /v1/library/tracks')).toBe(2);
    secondMount.unmount();
  });

  it('the cache is fully cleared, not merely marked stale — an infinite staleTime would otherwise let the remounted query reuse the previous user’s cached data with no fetch at all', async () => {
    await storeSessionFor('a');
    __http.reply('POST /auth/v1/logout', { status: 204 });
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
    await storeSessionFor('b');

    const secondMount = renderHook(() => useAuthenticatedLibraryQuery(), { wrapper });
    await waitFor(() => expect(secondMount.result.current.isSuccess).toBe(true));

    expect(__http.countFor('GET /v1/library/tracks')).toBe(2);
    secondMount.unmount();
  });
});

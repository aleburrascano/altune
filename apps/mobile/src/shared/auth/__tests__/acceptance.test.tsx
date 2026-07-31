import React from 'react';
import { QueryClient, QueryClientProvider, useQuery } from '@tanstack/react-query';
import { renderHook, act, waitFor } from '@testing-library/react-native';

import { apiFetch } from '@shared/api-client';
import { supabase } from '@shared/auth/supabaseClient';
import { useSignOut } from '../useSignOut';

const { __http } = require('../../../../jest/doubles/fetch.js');

function makeWrapper(queryClient: QueryClient) {
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
  };
}

describe('AC#4 — a session survives process death (in-process half: real storage adapter, module registry discarded, session restored)', () => {
  const FIXTURE_USER_ID = '11111111-1111-4111-8111-111111111111';

  function seedSession(): {
    access_token: string;
    refresh_token: string;
    expires_at: number;
    expires_in: number;
    token_type: string;
    user: { id: string };
  } {
    return {
      access_token: 'fixture-access-token',
      refresh_token: 'fixture-refresh-token',
      expires_at: Math.floor(Date.now() / 1000) + 3600,
      expires_in: 3600,
      token_type: 'bearer',
      user: { id: FIXTURE_USER_ID },
    };
  }

  beforeEach(() => {
    const { __secureStore } = require('expo-secure-store');
    __secureStore.reset();
    delete require.cache[require.resolve('../supabaseClient')];
  });

  it('restores the previously written session after the app "restarts" — no re-authentication is asked of the user', async () => {
    const first = require('../supabaseClient');
    await first.supabase.auth.getSession();
    await first.supabase.auth._saveSession(seedSession());
    await first.supabase.auth.stopAutoRefresh();

    delete require.cache[require.resolve('../supabaseClient')];

    const { useSession } = require('../useSession');
    const queryClient = new QueryClient();
    const { result, unmount } = renderHook(() => useSession(), {
      wrapper: makeWrapper(queryClient),
    });

    expect(result.current.status).toBe('loading');

    await waitFor(() => {
      expect(result.current.status).toBe('signed-in');
    });

    expect(result.current).toMatchObject({
      status: 'signed-in',
      session: { user: { id: FIXTURE_USER_ID } },
    });

    unmount();
    const second = require('../supabaseClient');
    await second.supabase.auth.stopAutoRefresh();
  });

  it('a device with no previously written session comes back signed-out, not stuck loading', async () => {
    const { useSession } = require('../useSession');
    const queryClient = new QueryClient();
    const { result, unmount } = renderHook(() => useSession(), {
      wrapper: makeWrapper(queryClient),
    });

    await waitFor(() => {
      expect(result.current.status).toBe('signed-out');
    });

    unmount();
    const client = require('../supabaseClient');
    await client.supabase.auth.stopAutoRefresh();
  });
});

describe('AC#5(b) — sign-out invalidates the React Query cache, verified by an authenticated query firing a fresh network fetch', () => {
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

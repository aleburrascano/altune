import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { renderHook, act } from '@testing-library/react-native';
import * as SecureStore from 'expo-secure-store';

import { supabase, useSignOut } from '@shared/auth';
import { recordLogs } from '@testing/fakes/log/inMemoryLogger';
const { __http } = require('@testing/doubles/fetch.js');

function makeWrapper(queryClient: QueryClient) {
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
  };
}

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
    recordLogs();
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

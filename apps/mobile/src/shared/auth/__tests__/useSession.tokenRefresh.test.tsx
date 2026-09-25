import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { renderHook, act } from '@testing-library/react-native';
import type { AuthChangeEvent, Session } from '@supabase/supabase-js';

import { useSession } from '../useSession';
import { supabase } from '../supabaseClient';
import {
  clearSessionExpired,
  getSessionExpired,
  markSessionExpired,
  stampCredentials,
} from '../sessionExpired';

jest.mock('../supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn(), onAuthStateChange: jest.fn() } },
}));

type Listener = (event: AuthChangeEvent, session: Session | null) => void;

function installAuth(): (event: AuthChangeEvent, session: Session | null) => void {
  let listener: Listener | null = null;
  (supabase.auth.onAuthStateChange as jest.Mock).mockImplementation((cb: Listener) => {
    listener = cb;
    return { data: { subscription: { unsubscribe: jest.fn() } } };
  });
  (supabase.auth.getSession as jest.Mock).mockReturnValue(new Promise<never>(() => {}));
  return (event, session) => {
    if (!listener) throw new Error('onAuthStateChange was never subscribed to');
    listener(event, session);
  };
}

function sessionFor(userId: string, accessToken = `token-${userId}`): Session {
  return {
    access_token: accessToken,
    refresh_token: `refresh-${userId}`,
    expires_in: 3600,
    token_type: 'bearer',
    user: { id: userId } as unknown as Session['user'],
  };
}

function renderSignedIn(userId: string) {
  const emit = installAuth();
  const queryClient = new QueryClient();
  const rendered = renderHook(() => useSession(), {
    wrapper: ({ children }: { children: React.ReactNode }) => (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    ),
  });
  act(() => emit('SIGNED_IN', sessionFor(userId)));
  return { emit, unmount: rendered.unmount };
}

let unmount: (() => void) | null = null;

beforeEach(() => {
  jest.clearAllMocks();
  clearSessionExpired();
});

afterEach(() => {
  unmount?.();
  unmount = null;
});

describe('a same-user token refresh lifts the session-expired notice', () => {
  it('clears the flag when supabase refreshes the token for the user already signed in', () => {
    const signedIn = renderSignedIn('user-a');
    unmount = signedIn.unmount;
    markSessionExpired();

    act(() => signedIn.emit('TOKEN_REFRESHED', sessionFor('user-a', 'rotated-token')));

    expect(getSessionExpired()).toBe(false);
  });

  it('keeps the flag when the same user is re-announced without a refresh', () => {
    const signedIn = renderSignedIn('user-a');
    unmount = signedIn.unmount;
    markSessionExpired();

    act(() => signedIn.emit('USER_UPDATED', sessionFor('user-a')));

    expect(getSessionExpired()).toBe(true);
  });

  it('a 401 stamped before the refresh cannot re-raise the notice after it', () => {
    const signedIn = renderSignedIn('user-a');
    unmount = signedIn.unmount;
    const stampBeforeRefresh = stampCredentials();

    act(() => signedIn.emit('TOKEN_REFRESHED', sessionFor('user-a', 'rotated-token')));
    markSessionExpired(stampBeforeRefresh);

    expect(getSessionExpired()).toBe(false);
  });
});

import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { renderHook, act } from '@testing-library/react-native';
import type { AuthChangeEvent, Session } from '@supabase/supabase-js';

import { useSession } from '../useSession';
import { supabase } from '../supabaseClient';
import { isTestAuthEnabled, testAuthBootSettled } from '../testAuthBoot';

jest.mock('../supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn(), onAuthStateChange: jest.fn() } },
}));

jest.mock('../testAuthBoot', () => ({
  isTestAuthEnabled: jest.fn(),
  testAuthBootSettled: jest.fn(),
}));

type Listener = (event: AuthChangeEvent, session: Session | null) => void;

function installAuth() {
  let listener: Listener | null = null;
  (supabase.auth.onAuthStateChange as jest.Mock).mockImplementation((cb: Listener) => {
    listener = cb;
    return { data: { subscription: { unsubscribe: jest.fn() } } };
  });
  const getSession = supabase.auth.getSession as jest.Mock;
  getSession.mockReset();
  getSession.mockResolvedValue({ data: { session: null } });
  return {
    getSession,
    emit(event: AuthChangeEvent, session: Session | null): void {
      if (!listener) throw new Error('onAuthStateChange was never subscribed to');
      listener(event, session);
    },
  };
}

function installTestLogin(enabled: boolean) {
  let settle: () => void = () => {};
  const settled = new Promise<void>((resolve) => {
    settle = resolve;
  });
  (isTestAuthEnabled as jest.Mock).mockReturnValue(enabled);
  (testAuthBootSettled as jest.Mock).mockReturnValue(enabled ? settled : Promise.resolve());
  return { settle };
}

function makeSession(userId: string): Session {
  return {
    access_token: `token-${userId}`,
    refresh_token: `refresh-${userId}`,
    expires_in: 3600,
    token_type: 'bearer',
    user: { id: userId } as unknown as Session['user'],
  };
}

function renderSession() {
  const queryClient = new QueryClient();
  return renderHook(() => useSession(), {
    wrapper: ({ children }: { children: React.ReactNode }) => (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    ),
  });
}

async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  });
}

describe('useSession while the test login is in flight', () => {
  it('stays loading through the empty initial session and goes straight to signed-in', async () => {
    const auth = installAuth();
    installTestLogin(true);
    const { result } = renderSession();
    const seen: string[] = [];

    auth.emit('INITIAL_SESSION', null);
    await flush();
    seen.push(result.current.status);

    act(() => auth.emit('SIGNED_IN', makeSession('test-user')));
    seen.push(result.current.status);

    expect(seen).toEqual(['loading', 'signed-in']);
    expect(auth.getSession).not.toHaveBeenCalled();
  });

  it('falls back to signed-out once a failed test login settles', async () => {
    const auth = installAuth();
    const login = installTestLogin(true);
    const { result } = renderSession();

    auth.emit('INITIAL_SESSION', null);
    await flush();
    expect(result.current).toEqual({ status: 'loading' });

    login.settle();
    await flush();

    expect(result.current).toEqual({ status: 'signed-out' });
  });

  it('does not hold anything when test auth is off', async () => {
    installAuth();
    installTestLogin(false);
    const { result } = renderSession();

    await flush();

    expect(result.current).toEqual({ status: 'signed-out' });
  });
});

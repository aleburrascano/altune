// Issue #1638: a recovery window must not outlive the session it was verified
// for. `forgetPreviousUsersLocalData` — the shared cleanup both `useSession`
// (on an identity change) and `useSignOut` (on an explicit sign-out) run — ends
// in `runSignOutCleanups()`, and recoveryUnlock registers `clearRecoveryUnlock`
// there, so both paths close the window. These tests drive the real hooks rather
// than the registry, because the registration is the thing that can go missing.
import React from 'react';
import type { AuthChangeEvent, Session } from '@supabase/supabase-js';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook } from '@testing-library/react-native';

import { supabase } from '@shared/auth/supabaseClient';
import { useSession } from '@shared/auth/useSession';
import { useSignOut } from '@shared/auth/useSignOut';

import { clearRecoveryUnlock, isRecoveryUnlocked, markRecoveryUnlocked } from '../recoveryUnlock';

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: {
    auth: { getSession: jest.fn(), onAuthStateChange: jest.fn(), signOut: jest.fn() },
  },
}));

const RECOVERING_USER = 'user-a';
const OTHER_USER = 'user-b';

type Listener = (event: AuthChangeEvent, session: Session | null) => void;

function installAuth() {
  const listeners: Listener[] = [];
  (supabase.auth.onAuthStateChange as jest.Mock).mockImplementation((callback: Listener) => {
    listeners.push(callback);
    return { data: { subscription: { unsubscribe: jest.fn() } } };
  });
  (supabase.auth.getSession as jest.Mock).mockReturnValue(new Promise<never>(() => {}));
  (supabase.auth.signOut as jest.Mock).mockResolvedValue({ error: null });

  return {
    emit(event: AuthChangeEvent, session: Session | null): void {
      const listener = listeners[listeners.length - 1];
      if (!listener) throw new Error('onAuthStateChange was never subscribed to');
      listener(event, session);
    },
  };
}

function sessionFor(userId: string): Session {
  return {
    access_token: `token-${userId}`,
    refresh_token: `refresh-${userId}`,
    expires_in: 3600,
    token_type: 'bearer',
    user: { id: userId } as unknown as Session['user'],
  };
}

function Wrapper({ children }: { children: React.ReactNode }) {
  return <QueryClientProvider client={new QueryClient()}>{children}</QueryClientProvider>;
}

const pendingUnmounts: (() => void)[] = [];

function renderSession() {
  const auth = installAuth();
  const rendered = renderHook(() => useSession(), { wrapper: Wrapper });
  pendingUnmounts.push(rendered.unmount);
  return auth;
}

beforeEach(() => {
  jest.clearAllMocks();
  clearRecoveryUnlock();
});

afterEach(() => {
  while (pendingUnmounts.length > 0) pendingUnmounts.pop()?.();
  clearRecoveryUnlock();
});

describe('a recovery window dies with the session it was verified for (#1638)', () => {
  it('an explicit sign-out closes a window an abandoned recovery left open', async () => {
    installAuth();
    markRecoveryUnlocked(RECOVERING_USER);
    const { result } = renderHook(() => useSignOut(), { wrapper: Wrapper });

    await act(async () => {
      await result.current.signOut();
    });

    expect(isRecoveryUnlocked(RECOVERING_USER)).toBe(false);
  });

  it('a sign-out that fails server-side still closes the window, because the local session is gone either way', async () => {
    installAuth();
    (supabase.auth.signOut as jest.Mock).mockRejectedValue(new Error('network down'));
    markRecoveryUnlocked(RECOVERING_USER);
    const { result } = renderHook(() => useSignOut(), { wrapper: Wrapper });

    await act(async () => {
      await result.current.signOut();
    });

    expect(isRecoveryUnlocked(RECOVERING_USER)).toBe(false);
  });

  it('a revoked session observed by useSession closes the window', () => {
    const auth = renderSession();
    act(() => auth.emit('PASSWORD_RECOVERY', sessionFor(RECOVERING_USER)));
    markRecoveryUnlocked(RECOVERING_USER);

    act(() => auth.emit('SIGNED_OUT', null));

    expect(isRecoveryUnlocked(RECOVERING_USER)).toBe(false);
  });

  it('a switch straight into another account closes the window, leaving it open for neither', () => {
    const auth = renderSession();
    act(() => auth.emit('PASSWORD_RECOVERY', sessionFor(RECOVERING_USER)));
    markRecoveryUnlocked(RECOVERING_USER);

    act(() => auth.emit('SIGNED_IN', sessionFor(OTHER_USER)));

    expect(isRecoveryUnlocked(RECOVERING_USER)).toBe(false);
    expect(isRecoveryUnlocked(OTHER_USER)).toBe(false);
  });
});

// The SDK notifies its auth-state subscribers from inside `verifyOtp`, before
// that call resolves, so the identity change the exchange causes always lands
// BEFORE completeAuthIntent marks the window. These pin that the cleanup cannot
// wipe the window the same exchange is about to open.
describe('the recovery exchange that opens the window does not close it', () => {
  it('leaves the window open on a cold start, where the exchange signs the recovering user in from signed-out', () => {
    const auth = renderSession();
    act(() => auth.emit('INITIAL_SESSION', null));

    act(() => auth.emit('PASSWORD_RECOVERY', sessionFor(RECOVERING_USER)));
    markRecoveryUnlocked(RECOVERING_USER);

    expect(isRecoveryUnlocked(RECOVERING_USER)).toBe(true);
  });

  it('leaves the window open when the token is refreshed for the same user mid-flow', () => {
    const auth = renderSession();
    act(() => auth.emit('PASSWORD_RECOVERY', sessionFor(RECOVERING_USER)));
    markRecoveryUnlocked(RECOVERING_USER);

    act(() => auth.emit('TOKEN_REFRESHED', sessionFor(RECOVERING_USER)));

    expect(isRecoveryUnlocked(RECOVERING_USER)).toBe(true);
  });
});

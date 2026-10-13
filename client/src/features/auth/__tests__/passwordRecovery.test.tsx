import { act, renderHook } from '@testing-library/react-native';
import React from 'react';
import type { AuthChangeEvent, Session } from '@supabase/supabase-js';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';

import { useSession } from '@shared/auth/useSession';
import { useSignOut } from '@shared/auth/useSignOut';

import {
  clearRecoveryUnlock,
  isRecoveryUnlocked,
  markRecoveryUnlocked,
  subscribeRecoveryUnlock,
} from '@features/auth/domain/passwordRecovery';
import { useRecoveryUnlocked } from '@features/auth/hooks/useRecoveryUnlocked';

import { createSupabaseAuthMock } from '@features/auth/__tests__/testUtils/authTestUtils';

const { getSession, onAuthStateChange, signOut } = createSupabaseAuthMock(
  'getSession',
  'onAuthStateChange',
  'signOut',
);

const VERIFIED_USER = 'user-a';
const OTHER_USER = 'user-b';

beforeEach(() => {
  clearRecoveryUnlock();
});

afterEach(() => {
  clearRecoveryUnlock();
});

const RECOVERY_UNLOCK_WINDOW_MS = 300_000;

describe('recoveryUnlock marker', () => {
  it('is locked by default', () => {
    expect(isRecoveryUnlocked(VERIFIED_USER)).toBe(false);
  });

  it('unlocks for the window after markRecoveryUnlocked', () => {
    const now = 1_000_000;
    markRecoveryUnlocked(VERIFIED_USER, now);

    expect(isRecoveryUnlocked(VERIFIED_USER, now)).toBe(true);
    expect(isRecoveryUnlocked(VERIFIED_USER, now + RECOVERY_UNLOCK_WINDOW_MS - 1)).toBe(true);
  });

  it('re-locks once the window has elapsed', () => {
    const now = 1_000_000;
    markRecoveryUnlocked(VERIFIED_USER, now);

    expect(isRecoveryUnlocked(VERIFIED_USER, now + RECOVERY_UNLOCK_WINDOW_MS)).toBe(false);
    expect(isRecoveryUnlocked(VERIFIED_USER, now + RECOVERY_UNLOCK_WINDOW_MS + 1)).toBe(false);
  });

  it('clearRecoveryUnlock re-locks immediately', () => {
    const now = 1_000_000;
    markRecoveryUnlocked(VERIFIED_USER, now);
    clearRecoveryUnlock();

    expect(isRecoveryUnlocked(VERIFIED_USER, now)).toBe(false);
  });

  it('notifies subscribers on mark and clear, and detaches on unmount', () => {
    let renders = 0;
    const { result, unmount } = renderHook(() => {
      renders += 1;
      return useRecoveryUnlocked(VERIFIED_USER);
    });

    expect(result.current).toBe(false);
    const before = renders;

    act(() => markRecoveryUnlocked(VERIFIED_USER));
    expect(result.current).toBe(true);
    expect(renders).toBeGreaterThan(before);

    act(() => clearRecoveryUnlock());
    expect(result.current).toBe(false);

    unmount();
    const afterUnmount = renders;
    act(() => markRecoveryUnlocked(VERIFIED_USER));
    expect(renders).toBe(afterUnmount);
  });

  it('stops notifying a listener once it unsubscribes, while others keep hearing', () => {
    const gone = jest.fn();
    const kept = jest.fn();
    const unsubscribeGone = subscribeRecoveryUnlock(gone);
    const unsubscribeKept = subscribeRecoveryUnlock(kept);

    markRecoveryUnlocked(VERIFIED_USER);
    expect(gone).toHaveBeenCalledTimes(1);
    expect(kept).toHaveBeenCalledTimes(1);

    unsubscribeGone();
    gone.mockClear();
    kept.mockClear();
    clearRecoveryUnlock();
    expect(gone).not.toHaveBeenCalled();
    expect(kept).toHaveBeenCalledTimes(1);

    unsubscribeKept();
    kept.mockClear();
    markRecoveryUnlocked(VERIFIED_USER);
    expect(gone).not.toHaveBeenCalled();
    expect(kept).not.toHaveBeenCalled();
  });

  it('reports locked when nothing was marked', () => {
    const { result } = renderHook(() => useRecoveryUnlocked(VERIFIED_USER));

    expect(result.current).toBe(false);
  });

  it('reports locked for a different user than the one marked', () => {
    markRecoveryUnlocked(VERIFIED_USER);
    const { result } = renderHook(() => useRecoveryUnlocked(OTHER_USER));

    expect(result.current).toBe(false);
  });

  it('clearRecoveryUnlock when already locked emits nothing extra', () => {
    let renders = 0;
    const { unmount } = renderHook(() => {
      renders += 1;
      return useRecoveryUnlocked(VERIFIED_USER);
    });
    const before = renders;

    act(() => clearRecoveryUnlock());

    expect(renders).toBe(before);
    unmount();
  });
});

describe('recoveryUnlock is bound to the identity the recovery was verified for', () => {
  it('stays locked for another account well inside the window', () => {
    const now = 1_000_000;
    markRecoveryUnlocked(VERIFIED_USER, now);

    expect(isRecoveryUnlocked(OTHER_USER, now)).toBe(false);
    expect(isRecoveryUnlocked(OTHER_USER, now + RECOVERY_UNLOCK_WINDOW_MS - 1)).toBe(false);
  });

  it('stays locked when nobody is signed in to compare against', () => {
    const now = 1_000_000;
    markRecoveryUnlocked(VERIFIED_USER, now);

    expect(isRecoveryUnlocked(null, now)).toBe(false);
  });

  it('hands the window to the second account when a second recovery is verified, and takes it from the first', () => {
    const now = 1_000_000;
    markRecoveryUnlocked(VERIFIED_USER, now);

    markRecoveryUnlocked(OTHER_USER, now);

    expect(isRecoveryUnlocked(OTHER_USER, now)).toBe(true);
    expect(isRecoveryUnlocked(VERIFIED_USER, now)).toBe(false);
  });

  it('re-renders a subscriber locked once the window it is watching belongs to someone else', () => {
    const { result, rerender } = renderHook(
      ({ userId }: { userId: string }) => useRecoveryUnlocked(userId),
      { initialProps: { userId: VERIFIED_USER } },
    );
    act(() => markRecoveryUnlocked(VERIFIED_USER));
    expect(result.current).toBe(true);

    rerender({ userId: OTHER_USER });

    expect(result.current).toBe(false);
  });
});

describe('recoveryUnlock is bounded by elapsed time no device clock can fake', () => {
  const MARKED_AT = 1_000_000;
  const MARKED_TICK = 5_000;
  const A_MONTH_MS = 30 * 24 * 60 * 60 * 1000;

  afterEach(() => {
    act(() => clearRecoveryUnlock());
    jest.useRealTimers();
  });

  it('stays locked when the wall clock is rolled back behind the moment it was marked', () => {
    markRecoveryUnlocked(VERIFIED_USER, MARKED_AT, MARKED_TICK);

    expect(isRecoveryUnlocked(VERIFIED_USER, MARKED_AT - 1, MARKED_TICK)).toBe(false);
    expect(isRecoveryUnlocked(VERIFIED_USER, MARKED_AT - A_MONTH_MS, MARKED_TICK)).toBe(false);
  });

  it('stays locked once the window of real elapsed time is spent, with the wall clock held still', () => {
    markRecoveryUnlocked(VERIFIED_USER, MARKED_AT, MARKED_TICK);

    expect(
      isRecoveryUnlocked(VERIFIED_USER, MARKED_AT, MARKED_TICK + RECOVERY_UNLOCK_WINDOW_MS),
    ).toBe(false);
  });

  it('stays unlocked while both clocks agree the window is still open', () => {
    markRecoveryUnlocked(VERIFIED_USER, MARKED_AT, MARKED_TICK);

    expect(
      isRecoveryUnlocked(
        VERIFIED_USER,
        MARKED_AT + RECOVERY_UNLOCK_WINDOW_MS - 1,
        MARKED_TICK + RECOVERY_UNLOCK_WINDOW_MS - 1,
      ),
    ).toBe(true);
  });

  it('stays locked after a rollback against the clocks it reads by default', () => {
    jest.useFakeTimers();
    jest.setSystemTime(MARKED_AT + A_MONTH_MS);
    markRecoveryUnlocked(VERIFIED_USER);

    jest.setSystemTime(MARKED_AT);

    expect(isRecoveryUnlocked(VERIFIED_USER)).toBe(false);
  });
});

describe('recoveryUnlock demotes a mounted subscriber the moment the window elapses', () => {
  beforeEach(() => {
    jest.useFakeTimers();
  });

  afterEach(() => {
    act(() => clearRecoveryUnlock());
    jest.useRealTimers();
  });

  it('flips from unlocked to locked once fake time passes the window', () => {
    const { result } = renderHook(() => useRecoveryUnlocked(VERIFIED_USER));
    act(() => markRecoveryUnlocked(VERIFIED_USER));
    expect(result.current).toBe(true);

    act(() => {
      jest.advanceTimersByTime(RECOVERY_UNLOCK_WINDOW_MS + 1);
    });

    expect(result.current).toBe(false);
  });

  it('holds a subscriber unlocked right up to the cutoff', () => {
    const { result } = renderHook(() => useRecoveryUnlocked(VERIFIED_USER));
    act(() => markRecoveryUnlocked(VERIFIED_USER));

    act(() => {
      jest.advanceTimersByTime(RECOVERY_UNLOCK_WINDOW_MS - 1);
    });

    expect(result.current).toBe(true);
  });

  it('locks a subscriber that never re-rendered once the cutoff passes', () => {
    const { result } = renderHook(() => useRecoveryUnlocked(VERIFIED_USER));
    act(() => markRecoveryUnlocked(VERIFIED_USER));
    expect(result.current).toBe(true);

    act(() => {
      jest.advanceTimersByTime(RECOVERY_UNLOCK_WINDOW_MS);
    });

    expect(result.current).toBe(false);
  });

  it('demotes a subscriber that mounted onto a window already open', () => {
    act(() => markRecoveryUnlocked(VERIFIED_USER));
    act(() => {
      jest.advanceTimersByTime(RECOVERY_UNLOCK_WINDOW_MS / 2);
    });

    const { result } = renderHook(() => useRecoveryUnlocked(VERIFIED_USER));
    expect(result.current).toBe(true);
    act(() => {
      jest.advanceTimersByTime(RECOVERY_UNLOCK_WINDOW_MS / 2);
    });

    expect(result.current).toBe(false);
  });

  it('gives a second verified recovery a full window of its own', () => {
    const { result } = renderHook(() => useRecoveryUnlocked(VERIFIED_USER));
    act(() => markRecoveryUnlocked(VERIFIED_USER));
    act(() => {
      jest.advanceTimersByTime(RECOVERY_UNLOCK_WINDOW_MS);
    });

    act(() => markRecoveryUnlocked(VERIFIED_USER));

    expect(result.current).toBe(true);
    act(() => {
      jest.advanceTimersByTime(RECOVERY_UNLOCK_WINDOW_MS);
    });
    expect(result.current).toBe(false);
  });

  it('stops emitting once the window was closed by hand, leaving no timer behind', () => {
    let renders = 0;
    renderHook(() => {
      renders += 1;
      return useRecoveryUnlocked(VERIFIED_USER);
    });
    act(() => markRecoveryUnlocked(VERIFIED_USER));
    act(() => clearRecoveryUnlock());
    const before = renders;

    act(() => {
      jest.advanceTimersByTime(2 * RECOVERY_UNLOCK_WINDOW_MS);
    });

    expect(renders).toBe(before);
  });
});

describe('closing the window on sign-out', () => {
  const RECOVERING_USER = 'user-a';

  type Listener = (event: AuthChangeEvent, session: Session | null) => void;

  function installAuth() {
    const listeners: Listener[] = [];
    onAuthStateChange.mockImplementation((callback: Listener) => {
      listeners.push(callback);
      return { data: { subscription: { unsubscribe: jest.fn() } } };
    });
    getSession.mockReturnValue(new Promise<never>(() => {}));
    signOut.mockResolvedValue({ error: null });

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
      signOut.mockRejectedValue(new Error('network down'));
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
});

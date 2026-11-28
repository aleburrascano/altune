import { monotonicNow } from '@shared/timing';
import type { RecoveryUnlockSource } from '@features/signIn/core/recoveryUnlockSource';

const RECOVERY_UNLOCK_WINDOW_MS = 300_000;

type RecoveryUnlock = { userId: string; openedAt: number; openedTick: number };

export interface RecoveryUnlockStore extends RecoveryUnlockSource {
  mark(userId: string, now?: number, tick?: number): void;
  isUnlocked(userId: string | null, now?: number, tick?: number): boolean;
}

type OnSignOut = (listener: () => void) => () => void;

function remainingWindowMs(open: RecoveryUnlock, now: number, tick: number): number {
  if (now < open.openedAt) return 0;
  const spent = Math.max(now - open.openedAt, tick - open.openedTick);
  return RECOVERY_UNLOCK_WINDOW_MS - spent;
}

export function createRecoveryUnlockStore(onSignOut: OnSignOut): RecoveryUnlockStore {
  let unlock: RecoveryUnlock | null = null;
  let windowTimer: ReturnType<typeof setTimeout> | undefined;
  const listeners = new Set<() => void>();

  function emit(): void {
    for (const listener of listeners) listener();
  }

  function stopWindowTimer(): void {
    clearTimeout(windowTimer);
    windowTimer = undefined;
  }

  function closeWindowWhenSpent(now: number, tick: number): void {
    stopWindowTimer();
    if (unlock === null || listeners.size === 0) return;
    const remaining = remainingWindowMs(unlock, now, tick);
    if (remaining <= 0) {
      clear();
      return;
    }
    windowTimer = setTimeout(() => closeWindowWhenSpent(Date.now(), monotonicNow()), remaining);
  }

  function mark(userId: string, now: number = Date.now(), tick: number = monotonicNow()): void {
    unlock = { userId, openedAt: now, openedTick: tick };
    closeWindowWhenSpent(now, tick);
    emit();
  }

  function clear(): void {
    if (unlock === null) return;
    unlock = null;
    stopWindowTimer();
    emit();
  }

  function isUnlocked(
    userId: string | null,
    now: number = Date.now(),
    tick: number = monotonicNow(),
  ): boolean {
    if (unlock === null) return false;
    return unlock.userId === userId && remainingWindowMs(unlock, now, tick) > 0;
  }

  function subscribe(listener: () => void): () => void {
    listeners.add(listener);
    closeWindowWhenSpent(Date.now(), monotonicNow());
    return () => {
      listeners.delete(listener);
      if (listeners.size === 0) stopWindowTimer();
    };
  }

  onSignOut(clear);

  return { mark, clear, isUnlocked, subscribe };
}

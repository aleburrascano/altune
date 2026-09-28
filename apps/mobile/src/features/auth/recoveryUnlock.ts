import { useSyncExternalStore } from 'react';

import { onSignOut } from '@shared/session/signOutCleanup';

export const RECOVERY_UNLOCK_WINDOW_MS = 5 * 60 * 1000;

const monotonicNow = (): number => performance.now();

type RecoveryUnlock = { userId: string; openedAt: number; openedTick: number };

let unlock: RecoveryUnlock | null = null;
let windowTimer: ReturnType<typeof setTimeout> | undefined;
const listeners = new Set<() => void>();

function emit(): void {
  for (const listener of listeners) listener();
}

function remainingWindowMs(open: RecoveryUnlock, now: number, tick: number): number {
  if (now < open.openedAt) return 0;
  const spent = Math.max(now - open.openedAt, tick - open.openedTick);
  return RECOVERY_UNLOCK_WINDOW_MS - spent;
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
    clearRecoveryUnlock();
    return;
  }
  windowTimer = setTimeout(() => closeWindowWhenSpent(Date.now(), monotonicNow()), remaining);
}

export function markRecoveryUnlocked(
  userId: string,
  now: number = Date.now(),
  tick: number = monotonicNow(),
): void {
  unlock = { userId, openedAt: now, openedTick: tick };
  closeWindowWhenSpent(now, tick);
  emit();
}

export function clearRecoveryUnlock(): void {
  if (unlock === null) return;
  unlock = null;
  stopWindowTimer();
  emit();
}

export function isRecoveryUnlocked(
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

export function useRecoveryUnlocked(userId: string | null): boolean {
  return useSyncExternalStore(
    subscribe,
    () => isRecoveryUnlocked(userId),
    () => isRecoveryUnlocked(userId),
  );
}

export function _listenerCountForTest(): number {
  return listeners.size;
}

onSignOut(clearRecoveryUnlock);

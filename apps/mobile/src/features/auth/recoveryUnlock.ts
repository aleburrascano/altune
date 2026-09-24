import { useSyncExternalStore } from 'react';

import { onSignOut } from '@shared/session/signOutCleanup';

export const RECOVERY_UNLOCK_WINDOW_MS = 5 * 60 * 1000;

/**
 * Monotonic milliseconds. Nothing the device owner can do to the calendar — a
 * manual change in Settings, an NTP resync, a timezone or DST shift — moves it,
 * so it measures time that really elapsed.
 */
const monotonicNow = (): number => performance.now();

// The window is the authority to change ONE account's password, so it names
// that account: `userId` is the identity the server verified the recovery token
// for. The moment it opened is recorded on both clocks, because neither alone
// bounds it: the wall clock is the user's to set backwards, and the monotonic
// tick can stall while the process is suspended (#1639). An abandoned flow used
// to leave a bare deadline behind that any account becoming active on the same
// process could spend (#1638) — an unlock with no identity to answer for is not
// a state this store can be in.
type RecoveryUnlock = { userId: string; openedAt: number; openedTick: number };

let unlock: RecoveryUnlock | null = null;
let windowTimer: ReturnType<typeof setTimeout> | undefined;
const listeners = new Set<() => void>();

function emit(): void {
  for (const listener of listeners) listener();
}

// Budget left, on whichever clock has spent the most of it, so no single clock
// moving can extend the window. A wall clock now reading earlier than the mark
// has been rolled back, which spends the window outright rather than granting
// the minutes the rollback invented (#1639).
function remainingWindowMs(open: RecoveryUnlock, now: number, tick: number): number {
  if (now < open.openedAt) return 0;
  const spent = Math.max(now - open.openedAt, tick - open.openedTick);
  return RECOVERY_UNLOCK_WINDOW_MS - spent;
}

function stopWindowTimer(): void {
  clearTimeout(windowTimer);
  windowTimer = undefined;
}

// A mounted AuthGate holds its snapshot until something emits, so without this
// the cutoff only landed on the next unrelated render (#1639). setTimeout counts
// down in real elapsed time, which is what the monotonic tick measures, but
// whether the window is actually spent on waking is re-decided against both.
// Only a subscriber needs waking — every read is already evaluated fresh — so
// with nobody watching this holds no timer rather than one per recovery.
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

// Open the window for `userId`, which must be the identity the server itself
// just verified the recovery token for — never one read back off whatever
// session is active afterwards. Call this ONLY once a recovery exchange has been
// confirmed good, never merely because the reset-password route was reached.
export function markRecoveryUnlocked(
  userId: string,
  now: number = Date.now(),
  tick: number = monotonicNow(),
): void {
  unlock = { userId, openedAt: now, openedTick: tick };
  closeWindowWhenSpent(now, tick);
  emit();
}

// Close the window immediately, e.g. after the password was updated so the
// screen cannot be re-entered without a fresh recovery link.
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

// A window opened before this subscriber mounted still owes it the cutoff, so
// mounting is what arms the timer; the last unmount gives it back.
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

// Process-lifetime state, so it outlives the tree AuthGate unmounts. Every
// identity change runs this — a sign-out, or a switch straight into another
// account — so a window an abandoned flow left open dies with the session it was
// verified for instead of waiting out its five minutes (#1638).
onSignOut(clearRecoveryUnlock);

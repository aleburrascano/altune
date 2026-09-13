import { useSyncExternalStore } from 'react';

// A verified password-recovery exchange unlocks the reset-password screen for a
// short window. Without this marker a bare `altune://reset-password` deep link
// would drop a signed-in user straight onto the "choose a new password" form and
// let them change the real account password with no recovery token ever
// verified (see #656). The window is short-lived so a marker left behind by an
// abandoned flow cannot be exploited minutes later; only completeAuthIntent
// sets it, and only after a recovery verifyOtp/setSession actually succeeds.
export const RECOVERY_UNLOCK_WINDOW_MS = 5 * 60 * 1000;

// Absolute deadline (ms epoch) until which the reset-password screen is
// unlocked; 0 means locked.
let unlockedUntil = 0;
const listeners = new Set<() => void>();

function emit(): void {
  for (const listener of listeners) listener();
}

// Open the unlock window. Call this ONLY once a recovery exchange has been
// confirmed good — never merely because the reset-password route was reached.
export function markRecoveryUnlocked(now: number = Date.now()): void {
  unlockedUntil = now + RECOVERY_UNLOCK_WINDOW_MS;
  emit();
}

// Close the window immediately, e.g. after the password was updated so the
// screen cannot be re-entered without a fresh recovery link.
export function clearRecoveryUnlock(): void {
  if (unlockedUntil === 0) return;
  unlockedUntil = 0;
  emit();
}

export function isRecoveryUnlocked(now: number = Date.now()): boolean {
  return unlockedUntil > now;
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

export function useRecoveryUnlocked(): boolean {
  return useSyncExternalStore(
    subscribe,
    () => isRecoveryUnlocked(),
    () => isRecoveryUnlocked(),
  );
}

export function _listenerCountForTest(): number {
  return listeners.size;
}

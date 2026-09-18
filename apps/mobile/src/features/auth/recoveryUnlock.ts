import { useSyncExternalStore } from 'react';

import { onSignOut } from '@shared/auth/signOutCleanup';

// A verified password-recovery exchange unlocks the reset-password screen for a
// short window. Without this marker a bare `altune://reset-password` deep link
// would drop a signed-in user straight onto the "choose a new password" form and
// let them change the real account password with no recovery token ever
// verified (see #656). The window is short-lived so a marker left behind by an
// abandoned flow cannot be exploited minutes later; only completeAuthIntent
// sets it, and only after a recovery verifyOtp/setSession actually succeeds.
export const RECOVERY_UNLOCK_WINDOW_MS = 5 * 60 * 1000;

// The window is the authority to change ONE account's password, so it names
// that account: `userId` is the identity the server verified the recovery token
// for, and `until` is the absolute deadline (ms epoch) it holds until. An
// abandoned flow used to leave a bare deadline behind that any account becoming
// active on the same process could spend (#1638) — an unlock with no identity to
// answer for is not a state this store can be in.
type RecoveryUnlock = { userId: string; until: number };

let unlock: RecoveryUnlock | null = null;
const listeners = new Set<() => void>();

function emit(): void {
  for (const listener of listeners) listener();
}

// Open the window for `userId`, which must be the identity the server itself
// just verified the recovery token for — never one read back off whatever
// session is active afterwards. Call this ONLY once a recovery exchange has been
// confirmed good, never merely because the reset-password route was reached.
export function markRecoveryUnlocked(userId: string, now: number = Date.now()): void {
  unlock = { userId, until: now + RECOVERY_UNLOCK_WINDOW_MS };
  emit();
}

// Close the window immediately, e.g. after the password was updated so the
// screen cannot be re-entered without a fresh recovery link.
export function clearRecoveryUnlock(): void {
  if (unlock === null) return;
  unlock = null;
  emit();
}

export function isRecoveryUnlocked(userId: string | null, now: number = Date.now()): boolean {
  if (unlock === null) return false;
  return unlock.userId === userId && unlock.until > now;
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
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

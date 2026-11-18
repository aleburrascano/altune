import { useSyncExternalStore } from 'react';

import type { RecoveryUnlockSource } from '@features/signIn/core/recoveryUnlockSource';

export function useRecoveryUnlocked(
  recovery: RecoveryUnlockSource,
  userId: string | null,
): boolean {
  function isUnlocked(): boolean {
    return recovery.isUnlocked(userId);
  }
  return useSyncExternalStore(recovery.subscribe, isUnlocked, isUnlocked);
}

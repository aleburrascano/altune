import { useSyncExternalStore } from 'react';

import { isRecoveryUnlocked, subscribeRecoveryUnlock } from '@features/auth/recoveryUnlock';

export function useRecoveryUnlocked(userId: string | null): boolean {
  return useSyncExternalStore(
    subscribeRecoveryUnlock,
    () => isRecoveryUnlocked(userId),
    () => isRecoveryUnlocked(userId),
  );
}

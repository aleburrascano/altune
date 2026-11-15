import { useSyncExternalStore } from 'react';

import {
  isRecoveryUnlocked,
  subscribeRecoveryUnlock,
} from '@features/signIn/domain/passwordRecovery';

export function useRecoveryUnlocked(userId: string | null): boolean {
  return useSyncExternalStore(
    subscribeRecoveryUnlock,
    () => isRecoveryUnlocked(userId),
    () => isRecoveryUnlocked(userId),
  );
}

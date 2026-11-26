import { useSyncExternalStore } from 'react';

import { getSessionExpired, subscribeSessionExpired } from '@shared/auth/core/sessionExpired';

export function useSessionExpired(): boolean {
  return useSyncExternalStore(subscribeSessionExpired, getSessionExpired, getSessionExpired);
}

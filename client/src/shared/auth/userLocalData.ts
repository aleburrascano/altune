import type { QueryClient } from '@tanstack/react-query';

import { setOutboxOwner } from '@shared/telemetry/outbox';

import { onIdentityChange, runSignOutCleanups } from '@shared/session/signOutCleanup';

import { clearSessionExpired } from '@shared/auth/sessionExpired';

export function forgetPreviousUsersLocalData(queryClient: QueryClient): void {
  queryClient.clear();
  clearSessionExpired();
  runSignOutCleanups();
}

let registered = false;

export function registerIdentityListeners(): void {
  if (registered) return;
  registered = true;
  onIdentityChange(setOutboxOwner);
}

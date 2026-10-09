import type { QueryClient } from '@tanstack/react-query';

import { setOutboxOwner } from '@shared/telemetry/outbox';
import { claimPinnedDownloads } from '@shared/offline/pinnedStore';
import { offlineDownloadsSupported } from '@shared/offline/offlineSupport';

import { onIdentityChange, runSignOutCleanups } from '@shared/session/signOutCleanup';

import { clearSessionExpired } from '@shared/auth/sessionExpired';

export function forgetPreviousUsersLocalData(queryClient: QueryClient): void {
  queryClient.clear();
  clearSessionExpired();
  runSignOutCleanups();
}

let registered = false;

function claimForSignedInUser(userId: string | null): void {
  if (userId !== null && offlineDownloadsSupported) claimPinnedDownloads(userId);
}

export function registerIdentityListeners(): void {
  if (registered) return;
  registered = true;
  onIdentityChange(setOutboxOwner);
  onIdentityChange(claimForSignedInUser);
}

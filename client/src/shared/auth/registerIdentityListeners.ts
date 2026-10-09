import { setOutboxOwner } from '@shared/telemetry/outbox';
import { claimPinnedDownloads } from '@shared/offline/pinnedStore';
import { offlineDownloadsSupported } from '@shared/offline/offlineSupport';

import { onIdentityChange } from '@shared/session/signOutCleanup';

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

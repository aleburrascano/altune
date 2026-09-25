import { setOutboxOwner } from '@shared/telemetry/outbox';
import { claimPinnedDownloads } from '@shared/offline/pinnedStore';

import { onIdentityChange } from '@shared/session/signOutCleanup';

let registered = false;

function claimForSignedInUser(userId: string | null): void {
  if (userId !== null) claimPinnedDownloads(userId);
}

export function registerIdentityListeners(): void {
  if (registered) return;
  registered = true;
  onIdentityChange(setOutboxOwner);
  onIdentityChange(claimForSignedInUser);
}

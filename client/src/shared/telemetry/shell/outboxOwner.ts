import { onIdentityChange } from '@shared/session';

import { setOutboxOwner } from '@shared/telemetry/outbox';

let following = false;

export function followSignedInUser(): void {
  if (following) return;
  following = true;
  onIdentityChange((userId) => setOutboxOwner(userId));
}

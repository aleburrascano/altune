import { onIdentityChange } from '@shared/session';

import { setOutboxOwner } from '@shared/telemetry/outbox';

export function followSignedInUser(): void {
  onIdentityChange((userId) => setOutboxOwner(userId));
}

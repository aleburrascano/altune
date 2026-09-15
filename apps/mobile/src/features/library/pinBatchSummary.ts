import { Alert } from 'react-native';

import type { PinBatchResult } from '@shared/offline/pinnedStore';

/**
 * Tells the user when a bulk download finished with failures. A clean batch stays
 * quiet: each row's offline indicator already shows it landed. Failed rows keep
 * their failed indicator and offer "Retry download" from the track menu.
 */
export function reportPinBatch({ requested, failed }: PinBatchResult): void {
  if (failed === 0) return;
  Alert.alert(
    'Some downloads failed',
    `${failed} of ${requested} downloads failed. Retry them from each track's menu.`,
  );
}

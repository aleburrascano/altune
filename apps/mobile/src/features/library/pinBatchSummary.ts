import { Alert } from 'react-native';

import type { PinBatchResult, UnpinBatchResult } from '@shared/offline/pinnedStore';

export function reportPinBatch({ requested, failed, refused }: PinBatchResult): void {
  if (refused === 'storage-full') {
    reportStorageFull();
    return;
  }
  if (failed === 0) return;
  Alert.alert(
    'Some downloads failed',
    `${failed} of ${requested} downloads failed. Retry them from each track's menu.`,
  );
}

export function reportUnpinBatch({ requested, failed }: UnpinBatchResult): void {
  if (failed === 0) return;
  Alert.alert(
    'Some downloads remain',
    `${failed} of ${requested} downloads could not be removed. Try removing them again.`,
  );
}

export function reportStorageFull(): void {
  Alert.alert(
    'Not enough storage',
    'Downloads are paused because storage is full. Remove some downloads or free up space, then try again.',
  );
}

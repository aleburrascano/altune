import { showFailureAlert } from '@shared/ui';

import type { PinBatchResult, UnpinBatchResult } from '@shared/offline/pinnedStore';

export function reportPinBatch({ requested, failed, refused }: PinBatchResult): void {
  if (refused === 'storage-full') {
    reportStorageFull();
    return;
  }
  if (failed === 0) return;
  showFailureAlert({
    surface: 'library.pin_batch',
    title: 'Some downloads failed',
    message: `${failed} of ${requested} downloads failed. Retry them from each track's menu.`,
  });
}

export function reportUnpinBatch({ requested, failed }: UnpinBatchResult): void {
  if (failed === 0) return;
  showFailureAlert({
    surface: 'library.unpin_batch',
    title: 'Some downloads remain',
    message: `${failed} of ${requested} downloads could not be removed. Try removing them again.`,
  });
}

export function reportStorageFull(): void {
  showFailureAlert({
    surface: 'library.storage_full',
    title: 'Not enough storage',
    message:
      'Downloads are paused because storage is full. Remove some downloads or free up space, then try again.',
  });
}

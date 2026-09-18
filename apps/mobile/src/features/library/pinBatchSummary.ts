import { Alert } from 'react-native';

import type { PinBatchResult, UnpinBatchResult } from '@shared/offline/pinnedStore';

/**
 * Tells the user when a bulk download finished with failures. A clean batch stays
 * quiet: each row's offline indicator already shows it landed. Failed rows keep
 * their failed indicator and offer "Retry download" from the track menu.
 */
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

/**
 * Tells the user when a bulk removal left downloads in place — a file the OS would not release, or
 * a batch that ran past its deadline. A clean removal stays quiet: the rows already show it.
 */
export function reportUnpinBatch({ requested, failed }: UnpinBatchResult): void {
  if (failed === 0) return;
  Alert.alert(
    'Some downloads remain',
    `${failed} of ${requested} downloads could not be removed. Try removing them again.`,
  );
}

/** Tells the user a download was refused because pinned storage is full. */
export function reportStorageFull(): void {
  Alert.alert(
    'Not enough storage',
    'Downloads are paused because storage is full. Remove some downloads or free up space, then try again.',
  );
}

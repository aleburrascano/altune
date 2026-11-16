import { useCallback } from 'react';
import type { TrackId, TrackResponse } from '@shared/catalog';
import {
  offlineDownloadsSupported,
  usePinnedStore,
  type PinAdmission,
  type PinBatchResult,
  type PinnedStatus,
  type UnpinBatchResult,
} from '@shared/offline';
import type { FailureAlert } from '@shared/ui';
import type { ShowFailure } from '@features/library/domain/libraryFailure';

export type LibraryOffline = {
  supported: boolean;
  statusOf: (trackId: TrackId) => PinnedStatus | undefined;
  pin: (trackId: TrackId) => PinAdmission;
  unpin: (trackId: TrackId) => void;
  pinMany: (trackIds: TrackId[]) => Promise<PinBatchResult>;
  unpinMany: (trackIds: TrackId[]) => Promise<UnpinBatchResult>;
};

export function useLibraryOffline(): LibraryOffline {
  const entries = usePinnedStore((s) => s.entries);
  const pin = usePinnedStore((s) => s.pin);
  const unpin = usePinnedStore((s) => s.unpin);
  const pinMany = usePinnedStore((s) => s.pinMany);
  const unpinMany = usePinnedStore((s) => s.unpinMany);
  const statusOf = useCallback(
    (trackId: TrackId) => (offlineDownloadsSupported ? entries[trackId]?.status : undefined),
    [entries],
  );
  return { supported: offlineDownloadsSupported, statusOf, pin, unpin, pinMany, unpinMany };
}

export function usePinnedStatus(trackId: TrackId): PinnedStatus | undefined {
  const status = usePinnedStore((s) => s.entries[trackId]?.status);
  return offlineDownloadsSupported ? status : undefined;
}

export function offlineEligibility(
  tracks: readonly TrackResponse[],
  statusOf: LibraryOffline['statusOf'],
): { downloadableIds: TrackId[]; pinnedCount: number; allPinned: boolean } {
  const downloadableIds: TrackId[] = [];
  for (const t of tracks) {
    if (t.acquisition_status === 'ready') downloadableIds.push(t.id);
  }
  const pinnedCount = downloadableIds.filter((id) => statusOf(id) === 'ready').length;
  const allPinned = downloadableIds.length > 0 && pinnedCount === downloadableIds.length;
  return { downloadableIds, pinnedCount, allPinned };
}

function pinBatchFailedAlert({ requested, failed }: PinBatchResult): FailureAlert {
  return {
    surface: 'library.pin_batch',
    title: 'Some downloads failed',
    message: `${failed} of ${requested} downloads failed. Retry them from each track's menu.`,
  };
}

export function reportPinBatch(showFailure: ShowFailure, batch: PinBatchResult): void {
  if (batch.refused === 'storage-full') return reportStorageFull(showFailure);
  if (batch.failed === 0) return;
  showFailure(pinBatchFailedAlert(batch));
}

function unpinBatchFailedAlert({ requested, failed }: UnpinBatchResult): FailureAlert {
  return {
    surface: 'library.unpin_batch',
    title: 'Some downloads remain',
    message: `${failed} of ${requested} downloads could not be removed. Try removing them again.`,
  };
}

export function reportUnpinBatch(showFailure: ShowFailure, batch: UnpinBatchResult): void {
  if (batch.failed === 0) return;
  showFailure(unpinBatchFailedAlert(batch));
}

export function reportStorageFull(showFailure: ShowFailure): void {
  showFailure({
    surface: 'library.storage_full',
    title: 'Not enough storage',
    message:
      'Downloads are paused because storage is full. Remove some downloads or free up space, then try again.',
  });
}

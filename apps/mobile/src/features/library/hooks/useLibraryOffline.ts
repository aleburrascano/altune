import { useCallback } from 'react';

import type { TrackId } from '@shared/api-client/ids';
import type { TrackResponse } from '@shared/api-client/types';
import {
  usePinnedStore,
  type PinAdmission,
  type PinBatchResult,
  type PinnedStatus,
  type UnpinBatchResult,
} from '@shared/offline/pinnedStore';

export type LibraryOffline = {
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
  const statusOf = useCallback((trackId: TrackId) => entries[trackId]?.status, [entries]);
  return { statusOf, pin, unpin, pinMany, unpinMany };
}

export function usePinnedStatus(trackId: TrackId): PinnedStatus | undefined {
  return usePinnedStore((s) => s.entries[trackId]?.status);
}

export function offlineEligibility(
  tracks: readonly TrackResponse[],
  statusOf: LibraryOffline['statusOf'],
): { downloadableIds: TrackId[]; pinnedCount: number; allPinned: boolean } {
  const downloadableIds = tracks.filter((t) => t.acquisition_status === 'ready').map((t) => t.id);
  const pinnedCount = downloadableIds.filter((id) => statusOf(id) === 'ready').length;
  const allPinned = downloadableIds.length > 0 && pinnedCount === downloadableIds.length;
  return { downloadableIds, pinnedCount, allPinned };
}

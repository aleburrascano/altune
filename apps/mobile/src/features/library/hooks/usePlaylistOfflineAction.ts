import type { TrackResponse } from '@shared/api-client/types';
import type { ContextMenuItem } from '@shared/ui/primitives/ContextMenu';

import { reportPinBatch, reportUnpinBatch } from '../pinBatchSummary';
import { offlineEligibility, useLibraryOffline } from './useLibraryOffline';

export function usePlaylistOfflineAction(tracks: readonly TrackResponse[]): ContextMenuItem {
  const offline = useLibraryOffline();
  const { downloadableIds, pinnedCount } = offlineEligibility(tracks, offline.statusOf);

  if (downloadableIds.length === 0) {
    return { label: 'Nothing to download yet', onPress: () => {} };
  }
  if (pinnedCount === downloadableIds.length) {
    return {
      label: 'Remove downloads',
      onPress: () => {
        void offline.unpinMany(downloadableIds).then(reportUnpinBatch);
      },
    };
  }
  const remaining = downloadableIds.length - pinnedCount;
  return {
    label: pinnedCount > 0 ? `Download rest (${remaining})` : `Download all (${remaining})`,
    onPress: () => {
      void offline.pinMany(downloadableIds).then(reportPinBatch);
    },
  };
}

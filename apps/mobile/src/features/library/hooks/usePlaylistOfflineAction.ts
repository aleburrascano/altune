import type { TrackResponse } from '@shared/api-client/types';
import { usePinnedStore } from '@shared/offline/pinnedStore';
import type { ContextMenuItem } from '@shared/ui/primitives/ContextMenu';

import { reportPinBatch, reportUnpinBatch } from '../pinBatchSummary';

// The playlist menu's offline entry. Only tracks that finished acquisition can be
// pinned; the label reflects how many of those are already downloaded.
export function usePlaylistOfflineAction(tracks: readonly TrackResponse[]): ContextMenuItem {
  const pinnedEntries = usePinnedStore((s) => s.entries);
  // Actions are stable store members, read at press time like trackMenu.ts does.
  const { pinMany, unpinMany } = usePinnedStore.getState();

  const downloadableIds = tracks.filter((t) => t.acquisition_status === 'ready').map((t) => t.id);
  const pinnedCount = downloadableIds.filter((id) => pinnedEntries[id]?.status === 'ready').length;

  if (downloadableIds.length === 0) {
    return { label: 'Nothing to download yet', onPress: () => {} };
  }
  if (pinnedCount === downloadableIds.length) {
    return {
      label: 'Remove downloads',
      onPress: () => {
        void unpinMany(downloadableIds).then(reportUnpinBatch);
      },
    };
  }
  const remaining = downloadableIds.length - pinnedCount;
  return {
    label: pinnedCount > 0 ? `Download rest (${remaining})` : `Download all (${remaining})`,
    onPress: () => {
      void pinMany(downloadableIds).then(reportPinBatch);
    },
  };
}

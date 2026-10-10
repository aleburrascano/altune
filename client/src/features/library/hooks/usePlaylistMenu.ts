import type { PlaylistId } from '@shared/apiClient/ids';
import type { Navigator } from '@shared/navigation';
import { useDeletePlaylist } from '@shared/playlists';
import { confirmDestructive } from '@shared/ui/dialog/dialog';
import { goBackOrToLibrary } from '@features/library/domain/libraryRoutes';
import type { TrackResponse } from '@shared/apiClient/trackResponse';
import type { ContextMenuItem } from '@shared/ui/primitives/ContextMenu';
import {
  offlineEligibility,
  reportPinBatch,
  reportUnpinBatch,
  useLibraryOffline,
} from '@features/library/hooks/useLibraryOffline';

const DELETE_PROMPT = {
  title: 'Delete Playlist',
  message: 'This cannot be undone.',
  confirmLabel: 'Delete',
};

export function usePlaylistDelete(playlistId: PlaylistId, navigator: Navigator): () => void {
  const deleteMut = useDeletePlaylist(playlistId);
  const onSuccess = () => goBackOrToLibrary(navigator);
  const onConfirm = () => deleteMut.mutate(undefined, { onSuccess });
  return () => confirmDestructive({ ...DELETE_PROMPT, onConfirm });
}

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

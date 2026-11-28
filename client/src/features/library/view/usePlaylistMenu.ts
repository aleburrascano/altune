import type { PlaylistId, TrackResponse } from '@shared/catalog';
import type { Navigator } from '@shared/navigation';
import { useDeletePlaylist } from '@shared/playlists';
import { goBackOrToLibrary } from '@features/library/core/libraryRoutes';
import type { ContextMenuItem } from '@shared/ui';
import {
  offlineEligibility,
  reportPinBatch,
  reportUnpinBatch,
  useLibraryOffline,
} from '@features/library/view/useLibraryOffline';
import { useLibraryPort } from '@features/library/view/LibraryProvider';

const DELETE_PROMPT = {
  title: 'Delete Playlist',
  message: 'This cannot be undone.',
  confirmLabel: 'Delete',
};

export function usePlaylistDelete(playlistId: PlaylistId, navigator: Navigator): () => void {
  const deleteMut = useDeletePlaylist(playlistId);
  const { confirmDestructive } = useLibraryPort();
  const onSuccess = () => goBackOrToLibrary(navigator);
  const onConfirm = () => deleteMut.mutate(undefined, { onSuccess });
  return () => confirmDestructive({ ...DELETE_PROMPT, onConfirm });
}

export function usePlaylistOfflineAction(tracks: readonly TrackResponse[]): ContextMenuItem {
  const offline = useLibraryOffline();
  const { showFailure } = useLibraryPort();
  const { downloadableIds, pinnedCount } = offlineEligibility(tracks, offline.statusOf);

  if (downloadableIds.length === 0) {
    return { label: 'Nothing to download yet', onPress: () => {} };
  }
  if (pinnedCount === downloadableIds.length) {
    return {
      label: 'Remove downloads',
      onPress: () => {
        void offline
          .unpinMany(downloadableIds)
          .then((batch) => reportUnpinBatch(showFailure, batch));
      },
    };
  }
  const remaining = downloadableIds.length - pinnedCount;
  return {
    label: pinnedCount > 0 ? `Download rest (${remaining})` : `Download all (${remaining})`,
    onPress: () => {
      void offline.pinMany(downloadableIds).then((batch) => reportPinBatch(showFailure, batch));
    },
  };
}

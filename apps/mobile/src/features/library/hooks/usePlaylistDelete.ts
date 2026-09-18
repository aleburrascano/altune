import type { useRouter } from 'expo-router';

import type { PlaylistId } from '@shared/api-client/ids';
import { useDeletePlaylist } from '@shared/playlists';
import { confirmDestructive } from '@shared/ui/confirmDestructive';

import { goBackOrToLibrary } from '../goBackOrToLibrary';

// Confirm-then-delete flow for a playlist. On success it leaves the detail screen,
// which is now showing a playlist that no longer exists.
export function usePlaylistDelete(
  playlistId: PlaylistId,
  router: ReturnType<typeof useRouter>,
): () => void {
  const deleteMut = useDeletePlaylist(playlistId);

  return () =>
    confirmDestructive({
      title: 'Delete Playlist',
      message: 'This cannot be undone.',
      confirmLabel: 'Delete',
      onConfirm: () =>
        deleteMut.mutate(undefined, {
          onSuccess: () => goBackOrToLibrary(router),
        }),
    });
}

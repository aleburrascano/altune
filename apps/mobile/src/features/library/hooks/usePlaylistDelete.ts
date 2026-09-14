import type { useRouter } from 'expo-router';

import type { PlaylistId } from '@shared/api-client/ids';
import { useDeletePlaylist } from '@shared/playlists';
import { confirmDestructive } from '@shared/ui/confirmDestructive';

// Confirm-then-delete flow for a playlist. On success it leaves the (now gone)
// detail screen: back when there is history, otherwise straight to the library.
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
          onSuccess: () => (router.canGoBack() ? router.back() : router.replace('/library')),
        }),
    });
}

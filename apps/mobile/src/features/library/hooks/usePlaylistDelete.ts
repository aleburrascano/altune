import type { PlaylistId } from '@shared/api-client/ids';
import type { Navigator } from '@shared/navigation';
import { useDeletePlaylist } from '@shared/playlists';
import { confirmDestructive } from '@shared/ui/confirmDestructive';

import { goBackOrToLibrary } from '../goBackOrToLibrary';

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

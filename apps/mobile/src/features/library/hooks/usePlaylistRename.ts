import { useState } from 'react';

import type { PlaylistId } from '@shared/api-client/ids';
import { useRenamePlaylist } from '@shared/playlists';

type PlaylistRenameState = {
  isEditing: boolean;
  editName: string;
  setEditName: (name: string) => void;
  startEditing: () => void;
  confirmRename: () => void;
};

// Inline rename flow for a playlist: seeds the edit field from the current name and
// only fires the mutation when the trimmed name is non-empty and actually changed.
export function usePlaylistRename(
  playlistId: PlaylistId,
  currentName: string | undefined,
): PlaylistRenameState {
  const [isEditing, setIsEditing] = useState(false);
  const [editName, setEditName] = useState('');
  const renameMut = useRenamePlaylist(playlistId);

  const startEditing = () => {
    if (currentName === undefined) return;
    setEditName(currentName);
    setIsEditing(true);
  };

  const confirmRename = () => {
    const trimmed = editName.trim();
    if (trimmed.length === 0 || trimmed === currentName) {
      setIsEditing(false);
      return;
    }
    renameMut.mutate(trimmed, { onSettled: () => setIsEditing(false) });
  };

  return { isEditing, editName, setEditName, startEditing, confirmRename };
}

import { useRef, useState } from 'react';

import type { PlaylistId } from '@shared/api-client/ids';
import { useRenamePlaylist } from '@shared/playlists';

type PlaylistRenameState = {
  isEditing: boolean;
  editName: string;
  setEditName: (name: string) => void;
  startEditing: () => void;
  confirmRename: () => void;
};

export function usePlaylistRename(
  playlistId: PlaylistId,
  currentName: string | undefined,
): PlaylistRenameState {
  const [isEditing, setIsEditing] = useState(false);
  const [editName, setEditName] = useState('');
  const renameMut = useRenamePlaylist(playlistId);
  const inFlight = useRef(false);

  const startEditing = () => {
    if (currentName === undefined) return;
    setEditName(currentName);
    setIsEditing(true);
  };

  const confirmRename = () => {
    if (inFlight.current) return;
    const trimmed = editName.trim();
    if (trimmed.length === 0 || trimmed === currentName) {
      setIsEditing(false);
      return;
    }
    inFlight.current = true;
    renameMut.mutate(trimmed, {
      onSettled: () => {
        inFlight.current = false;
        setIsEditing(false);
      },
    });
  };

  return { isEditing, editName, setEditName, startEditing, confirmRename };
}

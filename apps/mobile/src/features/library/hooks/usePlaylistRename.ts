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

// Inline rename flow for a playlist: seeds the edit field from the current name and
// only fires the mutation when the trimmed name is non-empty and actually changed.
//
// At most one rename is in flight. Return on the single-line field blurs it, so
// PlaylistHero's onSubmitEditing and onBlur both confirm in the same tick, and two
// requests for the same name race: a late failure from the first reverts the name the
// second just committed, because the shared revert matches on the name's value (#1698).
// The guard is a ref rather than `renameMut.isPending`, which both calls read before
// React has re-rendered — the very race a pending flag loses.
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

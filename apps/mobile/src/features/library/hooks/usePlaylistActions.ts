/**
 * usePlaylistActions — the playlist read + create concern for LibraryScreen.
 *
 * Pulls the playlists query, the create mutation, and the create-modal /
 * add-to-playlist sheet state out of LibraryScreen so the screen stops doing
 * data fetching inline (rn-component-patterns: fetching belongs in hooks).
 * The create mutation itself lives in usePlaylistMutations (one owner for
 * every playlist write); this hook keeps only the screen-facing state.
 */
import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';

import { getPlaylists } from '@shared/api-client/playlists';
import type { PlaylistResponse, TrackResponse } from '@shared/api-client/types';
import { playlistKeys } from '@shared/lib/query-keys';

import { useCreatePlaylist } from './usePlaylistMutations';

export type PlaylistActionsState = {
  playlists: PlaylistResponse[];
  createModalVisible: boolean;
  setCreateModalVisible: (visible: boolean) => void;
  addToPlaylistTrack: TrackResponse | null;
  setAddToPlaylistTrack: (track: TrackResponse | null) => void;
  createPlaylist: (name: string) => void;
  createLoading: boolean;
};

export function usePlaylistActions(): PlaylistActionsState {
  const [createModalVisible, setCreateModalVisible] = useState(false);
  const [addToPlaylistTrack, setAddToPlaylistTrack] = useState<TrackResponse | null>(null);

  const { data: playlistsData } = useQuery({
    queryKey: playlistKeys.list,
    queryFn: getPlaylists,
    staleTime: Infinity, // SSE-covered; event patches keep it fresh (F15)
  });
  const playlists = playlistsData?.items ?? [];

  const createMutation = useCreatePlaylist();

  return {
    playlists,
    createModalVisible,
    setCreateModalVisible,
    addToPlaylistTrack,
    setAddToPlaylistTrack,
    createPlaylist: (name) =>
      createMutation.mutate(name, { onSuccess: () => setCreateModalVisible(false) }),
    createLoading: createMutation.isPending,
  };
}

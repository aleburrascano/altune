/**
 * usePlaylistActions — the playlist read + create concern for LibraryScreen.
 *
 * Pulls the playlists query, the create mutation, and the create-modal /
 * add-to-playlist sheet state out of LibraryScreen so the screen stops doing
 * data fetching inline (rn-component-patterns: fetching belongs in hooks).
 */
import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { createPlaylist as createPlaylistApi, getPlaylists } from '@shared/api-client/playlists';
import type { PlaylistResponse, TrackResponse } from '@shared/api-client/types';

export type PlaylistActionsState = {
  playlists: PlaylistResponse[];
  createModalVisible: boolean;
  setCreateModalVisible: (visible: boolean) => void;
  addToPlaylistTrack: TrackResponse | null;
  setAddToPlaylistTrack: (track: TrackResponse | null) => void;
  createPlaylist: (name: string) => void;
  createLoading: boolean;
  invalidatePlaylists: () => void;
};

export function usePlaylistActions(): PlaylistActionsState {
  const queryClient = useQueryClient();
  const [createModalVisible, setCreateModalVisible] = useState(false);
  const [addToPlaylistTrack, setAddToPlaylistTrack] = useState<TrackResponse | null>(null);

  const { data: playlistsData } = useQuery({
    queryKey: ['playlists'],
    queryFn: getPlaylists,
  });
  const playlists = playlistsData?.items ?? [];

  const createMutation = useMutation({
    mutationFn: (name: string) => createPlaylistApi({ name }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['playlists'] });
      setCreateModalVisible(false);
    },
  });

  return {
    playlists,
    createModalVisible,
    setCreateModalVisible,
    addToPlaylistTrack,
    setAddToPlaylistTrack,
    createPlaylist: (name) => createMutation.mutate(name),
    createLoading: createMutation.isPending,
    invalidatePlaylists: () => {
      void queryClient.invalidateQueries({ queryKey: ['playlists'] });
    },
  };
}

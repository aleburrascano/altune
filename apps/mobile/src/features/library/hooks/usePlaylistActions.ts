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
  refetchPlaylists: () => void;
  isRefetchingPlaylists: boolean;
};

export function usePlaylistActions(): PlaylistActionsState {
  const [createModalVisible, setCreateModalVisible] = useState(false);
  const [addToPlaylistTrack, setAddToPlaylistTrack] = useState<TrackResponse | null>(null);

  const {
    data: playlistsData,
    isRefetching,
    refetch,
  } = useQuery({
    queryKey: playlistKeys.list,
    queryFn: getPlaylists,
    staleTime: Infinity,
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
    refetchPlaylists: () => {
      void refetch();
    },
    isRefetchingPlaylists: isRefetching,
  };
}

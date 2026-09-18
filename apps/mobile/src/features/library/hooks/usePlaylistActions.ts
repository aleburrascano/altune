import { useState } from 'react';
import { useInfiniteQuery } from '@tanstack/react-query';

import { getPlaylists } from '@shared/api-client/playlists';
import type { PlaylistResponse, TrackResponse } from '@shared/api-client/types';
import { playlistKeys } from '@shared/lib/query-keys';

import { useCreatePlaylist } from '@shared/playlists';

import { GROUP_PAGE_SIZE, nextGroupPageOffset } from '../groupPaging';

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
  loadMorePlaylists: () => void;
  isFetchingMorePlaylists: boolean;
};

export function usePlaylistActions(): PlaylistActionsState {
  const [createModalVisible, setCreateModalVisible] = useState(false);
  const [addToPlaylistTrack, setAddToPlaylistTrack] = useState<TrackResponse | null>(null);

  const {
    data: playlistsData,
    isRefetching,
    refetch,
    isFetchingNextPage,
    hasNextPage,
    fetchNextPage,
  } = useInfiniteQuery({
    queryKey: playlistKeys.paged,
    initialPageParam: 0,
    queryFn: ({ pageParam, signal }) =>
      getPlaylists({ limit: GROUP_PAGE_SIZE, offset: pageParam }, signal),
    getNextPageParam: (lastPage, _pages, lastOffset) =>
      nextGroupPageOffset(lastPage.items.length, lastOffset),
    staleTime: Infinity,
  });
  const playlists = playlistsData?.pages.flatMap((page) => page.items) ?? [];

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
    loadMorePlaylists: () => {
      if (hasNextPage && !isFetchingNextPage) void fetchNextPage();
    },
    isFetchingMorePlaylists: isFetchingNextPage,
  };
}

import { useEffect } from 'react';

import type { PlaylistResponse } from '@shared/api-client/types';

import type { PlaylistActionsState } from '@features/library/hooks/usePlaylistActions';
import type { ActiveView } from '@features/library/activeView';
import type { ListPaging, ListRefresh } from '@features/library/domain/refresh';
import { PLAYLIST_SORT_OPTIONS, type SortKey } from '@features/library/domain/sort';
import { PlaylistsGrid } from '@features/library/ui/PlaylistsGrid';

type PlaylistsViewDeps = {
  pl: PlaylistActionsState;
  sort: SortKey;
  onPlaylistPress: (playlist: PlaylistResponse) => void;
};

type PlaylistsView = {
  view: ActiveView;
  playlists: PlaylistResponse[];
};

function createdAtMillis(playlist: PlaylistResponse): number {
  const millis = Date.parse(playlist.created_at);
  return Number.isNaN(millis) ? Number.NEGATIVE_INFINITY : millis;
}

function sortPlaylistsByKey(playlists: PlaylistResponse[], key: SortKey): PlaylistResponse[] {
  const sorted = [...playlists];
  if (key === 'az') {
    return sorted.sort((a, b) => a.name.localeCompare(b.name));
  }
  return sorted.sort((a, b) => Math.sign(createdAtMillis(b) - createdAtMillis(a)) || 0);
}

export function usePlaylistsView({ pl, sort, onPlaylistPress }: PlaylistsViewDeps): PlaylistsView {
  const playlists = sortPlaylistsByKey(pl.playlists, sort);

  // The server orders by created_at only, so A to Z is right only over every page.
  // loadMorePlaylists is a no-op when there is no next page, one is in flight, or the last fetch failed.
  const { loadMorePlaylists, isFetchingMorePlaylists } = pl;
  const loadedCount = pl.playlists.length;
  useEffect(() => {
    if (sort === 'az') loadMorePlaylists();
  }, [sort, loadedCount, isFetchingMorePlaylists, loadMorePlaylists]);

  const refresh: ListRefresh = {
    refreshing: pl.isRefetchingPlaylists,
    onRefresh: pl.refetchPlaylists,
  };

  const paging: ListPaging = {
    onEndReached: pl.loadMorePlaylists,
    isFetchingNextPage: pl.isFetchingMorePlaylists,
    nextPageFailed: pl.morePlaylistsFailed,
    onRetryNextPage: pl.retryMorePlaylists,
  };

  return {
    playlists,
    view: {
      count: playlists.length,
      noun: 'playlist',
      options: PLAYLIST_SORT_OPTIONS,
      isLoading: false,
      error: pl.playlistsError,
      onRetry: pl.refetchPlaylists,
      content: (
        <PlaylistsGrid
          playlists={playlists}
          refresh={refresh}
          onPlaylistPress={onPlaylistPress}
          onCreatePress={() => pl.setCreateModalVisible(true)}
          paging={paging}
        />
      ),
    },
  };
}

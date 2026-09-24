import type { PlaylistResponse } from '@shared/api-client/types';

import type { PlaylistActionsState } from './usePlaylistActions';
import type { ActiveView } from '../activeView';
import type { ListRefresh } from '../refresh';
import { PLAYLIST_SORT_OPTIONS, type SortKey } from '../sort';
import { PlaylistsGrid } from '../ui/PlaylistsGrid';

export type PlaylistsViewDeps = {
  pl: PlaylistActionsState;
  sort: SortKey;
  onPlaylistPress: (playlist: PlaylistResponse) => void;
};

export type PlaylistsView = {
  view: ActiveView;
  playlists: PlaylistResponse[];
};

// Ordered by parsed instant, not the raw string: Go trims trailing zero fractional
// digits, so same-second timestamps can differ in precision where string order is
// wrong. Unparseable timestamps sort last.
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

  const refresh: ListRefresh = {
    refreshing: pl.isRefetchingPlaylists,
    onRefresh: pl.refetchPlaylists,
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
          onEndReached={pl.loadMorePlaylists}
          isFetchingNextPage={pl.isFetchingMorePlaylists}
        />
      ),
    },
  };
}

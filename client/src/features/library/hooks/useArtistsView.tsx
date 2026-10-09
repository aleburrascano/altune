import type { ArtistGroup } from '@shared/api-client/library';

import { useLibraryArtists } from '@features/library/hooks/useLibraryArtists';
import type { ActiveView } from '@features/library/domain/activeView';
import type { ListPaging, ListRefresh } from '@features/library/domain/refresh';
import { ARTIST_SORT_OPTIONS, type SortKey } from '@features/library/domain/sort';
import { ArtistsGrid } from '@features/library/ui/ArtistsGrid';

type ArtistsViewDeps = {
  query: string;
  sort: SortKey;
  isActive: boolean;
  onArtistPress: (artist: ArtistGroup) => void;
};

export function useArtistsView({
  query,
  sort,
  isActive,
  onArtistPress,
}: ArtistsViewDeps): ActiveView {
  const artistsState = useLibraryArtists(query, sort, isActive);

  const refresh: ListRefresh = {
    refreshing: artistsState.isRefetching,
    onRefresh: artistsState.refetch,
  };

  const paging: ListPaging = {
    onEndReached: artistsState.onEndReached,
    isFetchingNextPage: artistsState.isFetchingNextPage,
    nextPageFailed: artistsState.nextPageFailed,
    onRetryNextPage: artistsState.onRetryNextPage,
  };

  return {
    count: artistsState.artists.length,
    noun: 'artist',
    options: ARTIST_SORT_OPTIONS,
    isLoading: artistsState.isLoading,
    error: artistsState.artists.length === 0 ? artistsState.error : null,
    onRetry: artistsState.refetch,
    content: (
      <ArtistsGrid
        artists={artistsState.artists}
        emptyLabel={'No artists yet'}
        refresh={refresh}
        onArtistPress={onArtistPress}
        paging={paging}
      />
    ),
  };
}

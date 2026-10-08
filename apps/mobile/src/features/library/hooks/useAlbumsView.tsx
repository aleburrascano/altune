import type { AlbumGroup } from '@shared/api-client/library';

import { useLibraryAlbums } from '@features/library/hooks/useLibraryAlbums';
import type { ActiveView } from '@features/library/activeView';
import type { ListPaging, ListRefresh } from '@features/library/domain/refresh';
import { ALBUM_SORT_OPTIONS, type SortKey } from '@features/library/domain/sort';
import { AlbumsGrid } from '@features/library/ui/AlbumsGrid';

type AlbumsViewDeps = {
  query: string;
  sort: SortKey;
  isActive: boolean;
  onAlbumPress: (album: AlbumGroup) => void;
};

export function useAlbumsView({ query, sort, isActive, onAlbumPress }: AlbumsViewDeps): ActiveView {
  const albumsState = useLibraryAlbums(query, sort, isActive);

  const refresh: ListRefresh = {
    refreshing: albumsState.isRefetching,
    onRefresh: albumsState.refetch,
  };

  const paging: ListPaging = {
    onEndReached: albumsState.onEndReached,
    isFetchingNextPage: albumsState.isFetchingNextPage,
    nextPageFailed: albumsState.nextPageFailed,
    onRetryNextPage: albumsState.onRetryNextPage,
  };

  return {
    count: albumsState.albums.length,
    noun: 'album',
    options: ALBUM_SORT_OPTIONS,
    isLoading: albumsState.isLoading,
    error: albumsState.albums.length === 0 ? albumsState.error : null,
    onRetry: albumsState.refetch,
    content: (
      <AlbumsGrid
        albums={albumsState.albums}
        emptyLabel={'No albums yet'}
        refresh={refresh}
        onAlbumPress={onAlbumPress}
        paging={paging}
      />
    ),
  };
}

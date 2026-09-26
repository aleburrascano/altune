import type { AlbumGroup } from '@shared/api-client/library';

import { useLibraryAlbums } from './useLibraryAlbums';
import type { ActiveView } from '../activeView';
import type { ListPaging, ListRefresh } from '../refresh';
import { ALBUM_SORT_OPTIONS, type SortKey } from '../sort';
import { AlbumsGrid } from '../ui/AlbumsGrid';

export type AlbumsViewDeps = {
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

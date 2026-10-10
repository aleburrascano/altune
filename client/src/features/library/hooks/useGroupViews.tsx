import type { AlbumGroup, ArtistGroup } from '@shared/apiClient/library';
import { useLibraryAlbums, useLibraryArtists } from '@features/library/hooks/useLibraryGroups';
import {
  type ActiveView,
  type ListPaging,
  type ListRefresh,
  ALBUM_SORT_OPTIONS,
  type SortKey,
  ARTIST_SORT_OPTIONS,
} from '@features/library/domain/libraryView';
import { AlbumsGrid, ArtistsGrid } from '@features/library/ui/GroupGrids';

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

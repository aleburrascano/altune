import type { ArtistGroup } from '@shared/api-client/library';

import { useLibraryArtists } from './useLibraryHome';
import type { ActiveView } from '../activeView';
import type { ListRefresh } from '../refresh';
import { ARTIST_SORT_OPTIONS, type SortKey } from '../sort';
import { ArtistsGrid } from '../ui/ArtistsGrid';

export type ArtistsViewDeps = {
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

  return {
    count: artistsState.artists.length,
    noun: 'artist',
    options: ARTIST_SORT_OPTIONS,
    isLoading: artistsState.isLoading,
    error: artistsState.error,
    onRetry: artistsState.refetch,
    content: (
      <ArtistsGrid
        artists={artistsState.artists}
        emptyLabel={'No artists yet'}
        refresh={refresh}
        onArtistPress={onArtistPress}
      />
    ),
  };
}

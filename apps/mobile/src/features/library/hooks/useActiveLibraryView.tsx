import type { ReactElement } from 'react';
import type { useRouter } from 'expo-router';

import type { TrackResponse, PlaylistResponse } from '@shared/api-client/types';
import { isCurrentlyPlaying } from '@shared/playback/isCurrentlyPlaying';
import { buildPlayableQueue } from '@shared/playback/playFromList';
import type { usePlayback } from '@shared/playback/usePlayback';
import type { useQueuePlayback } from '@shared/playback/useQueuePlayback';
import type { MenuAnchor } from '@shared/ui/primitives/menuPlacement';

import type { PlaylistActionsState } from './usePlaylistActions';
import { useLibraryAlbums, useLibraryArtists, useLibraryTracks } from './useLibraryHome';
import type { useRetryAcquisition } from './useRetryAcquisition';
import type { Selection } from '../useSelection';
import { AlbumsGrid } from '../ui/AlbumsGrid';
import { ArtistsGrid } from '../ui/ArtistsGrid';
import type { LibraryChip } from '../ui/LibraryChips';
import { PlaylistsGrid } from '../ui/PlaylistsGrid';
import type { ListRefresh } from '../ui/refresh';
import {
  ALBUM_SORT_OPTIONS,
  ARTIST_SORT_OPTIONS,
  PLAYLIST_SORT_OPTIONS,
  TRACK_SORT_OPTIONS,
  type SortKey,
} from '../ui/sort';
import { TracksList } from '../ui/TracksList';
import type { useLibraryNavigation } from '../ui/useLibraryNavigation';

export type ActiveView = {
  content: ReactElement;
  count: number;
  noun: string;
  options: { key: SortKey; label: string }[];
  isLoading: boolean;
  error: Error | null;
  onRetry: () => void;
};

export type ActiveLibraryViewDeps = {
  pl: PlaylistActionsState;
  router: ReturnType<typeof useRouter>;
  navigation: ReturnType<typeof useLibraryNavigation>;
  selection: Selection;
  queue: ReturnType<typeof useQueuePlayback>;
  playback: ReturnType<typeof usePlayback>;
  retryMutation: ReturnType<typeof useRetryAcquisition>;
  onTrackMore: (track: TrackResponse, anchor: MenuAnchor) => void;
};

export type ActiveLibraryView = {
  active: ActiveView;
  tracks: TrackResponse[];
  playlists: PlaylistResponse[];
};

function sortPlaylistsByKey<T extends { name: string; created_at: string }>(
  playlists: T[],
  key: SortKey,
): T[] {
  const sorted = [...playlists];
  if (key === 'az') {
    return sorted.sort((a, b) => a.name.localeCompare(b.name));
  }
  return sorted.sort((a, b) => b.created_at.localeCompare(a.created_at));
}

export function useActiveLibraryView(
  chip: LibraryChip,
  sortByChip: Record<LibraryChip, SortKey>,
  query: string,
  deps: ActiveLibraryViewDeps,
): ActiveLibraryView {
  const { pl, router, navigation, selection, queue, playback, retryMutation, onTrackMore } = deps;

  const tracksState = useLibraryTracks(query, sortByChip.tracks, chip === 'tracks');
  const albumsState = useLibraryAlbums(query, sortByChip.albums, chip === 'albums');
  const artistsState = useLibraryArtists(query, sortByChip.artists, chip === 'artists');

  const playlists = sortPlaylistsByKey(pl.playlists, sortByChip.playlists);

  const playWholeLibraryFrom = async (track: TrackResponse): Promise<void> => {
    const all = await tracksState.loadAll();
    const { playable, startIndex } = buildPlayableQueue(all, track.id);
    queue.playFromList(playable, startIndex, { kind: 'library' });
  };

  const active = buildActiveView();

  return { active, tracks: tracksState.tracks, playlists };

  function buildActiveView(): ActiveView {
    switch (chip) {
      case 'playlists': {
        const refresh: ListRefresh = {
          refreshing: pl.isRefetchingPlaylists,
          onRefresh: pl.refetchPlaylists,
        };
        return {
          count: playlists.length,
          noun: 'playlist',
          options: PLAYLIST_SORT_OPTIONS,
          isLoading: false,
          error: null,
          onRetry: pl.refetchPlaylists,
          content: (
            <PlaylistsGrid
              playlists={playlists}
              refresh={refresh}
              onPlaylistPress={(playlist) => router.push(`/library/playlist/${playlist.id}`)}
              onCreatePress={() => pl.setCreateModalVisible(true)}
            />
          ),
        };
      }
      case 'tracks': {
        const refresh: ListRefresh = {
          refreshing: tracksState.isRefetching,
          onRefresh: tracksState.refetch,
        };
        return {
          count: tracksState.tracks.length === 0 ? 0 : tracksState.total,
          noun: 'track',
          options: TRACK_SORT_OPTIONS,
          isLoading: tracksState.isLoading,
          error: tracksState.error,
          onRetry: tracksState.refetch,
          content: (
            <TracksList
              tracks={tracksState.tracks}
              emptyLabel={'No tracks yet'}
              refresh={refresh}
              onEndReached={tracksState.onEndReached}
              isFetchingNextPage={tracksState.isFetchingNextPage}
              onPlay={(track) => void playWholeLibraryFrom(track)}
              onPress={navigation.navigateToTrack}
              onMore={onTrackMore}
              onRetry={(track) => retryMutation.mutate(track.id)}
              retryingTrackId={retryMutation.isPending ? retryMutation.variables : undefined}
              isPlaying={(id) => isCurrentlyPlaying(playback, { kind: 'library', trackId: id })}
              selection={selection}
            />
          ),
        };
      }
      case 'albums': {
        const refresh: ListRefresh = {
          refreshing: albumsState.isRefetching,
          onRefresh: albumsState.refetch,
        };
        return {
          count: albumsState.albums.length,
          noun: 'album',
          options: ALBUM_SORT_OPTIONS,
          isLoading: albumsState.isLoading,
          error: albumsState.error,
          onRetry: albumsState.refetch,
          content: (
            <AlbumsGrid
              albums={albumsState.albums}
              emptyLabel={'No albums yet'}
              refresh={refresh}
              onAlbumPress={navigation.navigateToAlbum}
            />
          ),
        };
      }
      case 'artists': {
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
              onArtistPress={navigation.navigateToArtist}
            />
          ),
        };
      }
    }
  }
}

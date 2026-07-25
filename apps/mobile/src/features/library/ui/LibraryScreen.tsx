import { useRouter } from 'expo-router';
import { useState, type ReactElement } from 'react';
import { Alert, StyleSheet, View } from 'react-native';

import type { TrackResponse } from '@shared/api-client/types';
import { isNetworkError } from '@shared/lib/isNetworkError';
import { isCurrentlyPlaying } from '@shared/playback/isCurrentlyPlaying';
import { buildPlayableQueue } from '@shared/playback/playFromList';
import { usePlayback } from '@shared/playback/usePlayback';
import { useQueuePlayback } from '@shared/playback/useQueuePlayback';
import { Button, Screen, Skeleton, Text, spacing, useTheme } from '@shared/ui';
import { useAnnounceChange } from '@shared/ui/useAnnounceChange';
import { ContextMenu } from '@shared/ui/primitives/ContextMenu';
import { SearchBar } from '@shared/ui/primitives/SearchBar';
import type { MenuAnchor } from '@shared/ui/primitives/menuPlacement';

import { useDeleteTrack } from '../hooks/useDeleteTrack';
import {
  useLibraryAlbums,
  useLibraryArtists,
  useLibraryIsEmpty,
  useLibraryTracks,
} from '../hooks/useLibraryHome';
import { useLibrarySearch } from '../hooks/useLibrarySearch';
import { usePlaylistActions } from '../hooks/usePlaylistActions';
import { useRetryAcquisition } from '../hooks/useRetryAcquisition';
import { _viewForState } from '../state';
import { AddToPlaylistSheet } from './AddToPlaylistSheet';
import { AlbumsGrid } from './AlbumsGrid';
import { ArtistsGrid } from './ArtistsGrid';
import { CreatePlaylistModal } from './CreatePlaylistModal';
import { LibraryChips, type LibraryChip } from './LibraryChips';
import { LibraryHeader } from './LibraryHeader';
import { LibraryNoResults } from './LibraryNoResults';
import { PlaylistsGrid } from './PlaylistsGrid';
import type { ListRefresh } from './refresh';
import { SortControl } from './SortControl';
import { buildTrackMenuItems } from './trackMenu';
import { TracksList } from './TracksList';
import {
  ALBUM_SORT_OPTIONS,
  ARTIST_SORT_OPTIONS,
  PLAYLIST_SORT_OPTIONS,
  TRACK_SORT_OPTIONS,
  type SortKey,
} from './sort';
import { useLibraryNavigation } from './useLibraryNavigation';

const DEFAULT_SORTS: Record<LibraryChip, SortKey> = {
  playlists: 'recent',
  tracks: 'recent',
  albums: 'az',
  artists: 'az',
};

type ActiveView = {
  content: ReactElement;
  count: number;
  noun: string;
  options: { key: SortKey; label: string }[];
  isLoading: boolean;
  error: Error | null;
  onRetry: () => void;
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

export function LibraryScreen(): ReactElement {
  const router = useRouter();
  const theme = useTheme();
  const pl = usePlaylistActions();
  const search = useLibrarySearch();
  const { navigateToTrack, navigateToAlbum, navigateToArtist } = useLibraryNavigation(router);
  const deleteMutation = useDeleteTrack();
  const retryMutation = useRetryAcquisition();
  const playback = usePlayback();
  const queue = useQueuePlayback();

  const [chip, setChip] = useState<LibraryChip>('playlists');
  const [sortByChip, setSortByChip] = useState<Record<LibraryChip, SortKey>>(DEFAULT_SORTS);
  const [action, setAction] = useState<{ track: TrackResponse; anchor: MenuAnchor } | null>(null);
  const [searchFocused, setSearchFocused] = useState(false);

  const tracksState = useLibraryTracks(search.query, sortByChip.tracks, chip === 'tracks');
  const albumsState = useLibraryAlbums(search.query, sortByChip.albums, chip === 'albums');
  const artistsState = useLibraryArtists(search.query, sortByChip.artists, chip === 'artists');
  const libraryIsEmpty = useLibraryIsEmpty();

  const confirmRemoveTrack = (track: TrackResponse): void => {
    Alert.alert('Remove from Library', `Remove "${track.title}" from your library?`, [
      { text: 'Cancel', style: 'cancel' },
      { text: 'Remove', style: 'destructive', onPress: () => deleteMutation.mutate(track.id) },
    ]);
  };

  const trackMenuItems = (track: TrackResponse) =>
    buildTrackMenuItems(track, {
      queue,
      onViewDetails: () => navigateToTrack(track),
      onAddToPlaylist: () => pl.setAddToPlaylistTrack(track),
      danger: { label: 'Remove from Library', onPress: () => confirmRemoveTrack(track) },
    });

  const sortKey = sortByChip[chip];
  const setSort = (key: SortKey): void => setSortByChip((prev) => ({ ...prev, [chip]: key }));

  const playlists = sortPlaylistsByKey(pl.playlists, sortByChip.playlists);
  const active = buildActiveView();

  useAnnounceChange(
    search.hasQuery ? `${active.count} ${active.count === 1 ? 'result' : 'results'}` : '',
  );

  const view = _viewForState({
    isLoading: active.isLoading,
    error: active.error,
    items: active.count === 0 ? [] : [active.count],
  });

  if (view === 'loading') {
    return (
      <Screen>
        <LibraryHeader />
        <View testID="library-loading" style={styles.skeletonGrid}>
          {[0, 1, 2, 3].map((i) => (
            <Skeleton key={i} width="47%" height={140} radius={8} />
          ))}
        </View>
      </Screen>
    );
  }

  if (view === 'error') {
    return (
      <Screen>
        <LibraryHeader />
        <View testID="library-error" style={styles.center}>
          <Text variant="title">Couldn&apos;t load your library</Text>
          <Text variant="label" tone="secondary" style={styles.centerSub}>
            {isNetworkError(active.error)
              ? 'You appear to be offline. Your library is safe — reconnect and try again.'
              : 'Something went wrong on our end. Try again in a moment.'}
          </Text>
          <Button testID="library-retry" label="Retry" onPress={active.onRetry} />
        </View>
      </Screen>
    );
  }

  if (view === 'empty' && !search.hasQuery && playlists.length === 0 && libraryIsEmpty) {
    return (
      <Screen>
        <LibraryHeader />
        <View testID="library-empty" style={styles.center}>
          <Text variant="title">Your library is empty</Text>
          <Text variant="label" tone="secondary" style={styles.centerSub}>
            Tracks you add will show up here.
          </Text>
          <Button label="Discover Music" onPress={() => router.push('/discover')} />
        </View>
      </Screen>
    );
  }

  return (
    <Screen>
      <LibraryHeader />
      <SearchBar
        value={search.inputValue}
        onChangeText={search.onChangeText}
        onSubmitEditing={search.onSubmit}
        onClear={search.onClear}
        onFocus={() => setSearchFocused(true)}
        onBlur={() => setSearchFocused(false)}
        focused={searchFocused}
        placeholder="Search your library"
        testID="library-search-input"
        theme={theme}
      />
      <LibraryChips value={chip} onChange={setChip} />
      <SortControl
        count={active.count}
        noun={active.noun}
        sortKey={sortKey}
        options={active.options}
        onSortChange={setSort}
      />
      <View style={styles.body}>
        {search.hasQuery && active.count === 0 ? (
          <LibraryNoResults query={search.query} onClear={search.onClear} />
        ) : (
          active.content
        )}
      </View>

      <CreatePlaylistModal
        visible={pl.createModalVisible}
        onClose={() => pl.setCreateModalVisible(false)}
        onCreate={pl.createPlaylist}
        loading={pl.createLoading}
      />
      <AddToPlaylistSheet
        visible={pl.addToPlaylistTrack != null}
        trackId={pl.addToPlaylistTrack?.id ?? ''}
        trackTitle={
          pl.addToPlaylistTrack != null
            ? `${pl.addToPlaylistTrack.title} — ${pl.addToPlaylistTrack.artist}`
            : ''
        }
        onClose={() => pl.setAddToPlaylistTrack(null)}
      />
      <ContextMenu
        visible={action != null}
        anchor={action?.anchor}
        items={action != null ? trackMenuItems(action.track) : []}
        onClose={() => setAction(null)}
      />
    </Screen>
  );

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
              onPlay={(track) => {
                const { playable, startIndex } = buildPlayableQueue(tracksState.tracks, track.id);
                queue.playFromList(playable, startIndex, { kind: 'library' });
              }}
              onPress={navigateToTrack}
              onMore={(track, anchor) => setAction({ track, anchor })}
              onRetry={(track) => retryMutation.mutate(track.id)}
              retryingTrackId={retryMutation.isPending ? retryMutation.variables : undefined}
              isPlaying={(id) => isCurrentlyPlaying(playback, { kind: 'library', trackId: id })}
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
              onAlbumPress={navigateToAlbum}
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
              onArtistPress={navigateToArtist}
            />
          ),
        };
      }
    }
  }
}

const styles = StyleSheet.create({
  body: { flex: 1 },
  center: { flex: 1, alignItems: 'center', justifyContent: 'center', padding: spacing['2xl'] },
  centerSub: { marginTop: spacing.xs, marginBottom: spacing.lg, textAlign: 'center' },
  skeletonGrid: {
    flexDirection: 'row',
    flexWrap: 'wrap',
    gap: spacing.md,
    paddingTop: spacing.xl,
  },
});

import { useRouter } from 'expo-router';
import { useState, type ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';

import type { TrackResponse } from '@shared/api-client/types';
import { isCurrentlyPlaying } from '@shared/playback/isCurrentlyPlaying';
import { buildPlayableQueue } from '@shared/playback/playFromList';
import { usePlayback } from '@shared/playback/usePlayback';
import { useQueuePlayback } from '@shared/playback/useQueuePlayback';
import { Button, Screen, SearchBar, Skeleton, Text, spacing, useTheme } from '@shared/ui';
import { ContextMenu } from '@shared/ui/primitives/ContextMenu';
import type { MenuAnchor } from '@shared/ui/primitives/menuPlacement';

import { useDeleteTrack } from '../hooks/useDeleteTrack';
import { useLibraryHome } from '../hooks/useLibraryHome';
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
import { SortControl } from './SortControl';
import { buildTrackMenuItems } from './trackMenu';
import { TracksList } from './TracksList';
import {
  ALBUM_SORT_OPTIONS,
  ARTIST_SORT_OPTIONS,
  PLAYLIST_SORT_OPTIONS,
  TRACK_SORT_OPTIONS,
  sortAlbums,
  sortArtists,
  sortPlaylists,
  sortTracks,
  type SortKey,
} from './sort';
import { useLibraryNavigation } from './useLibraryNavigation';

const DEFAULT_SORTS: Record<LibraryChip, SortKey> = {
  playlists: 'recent',
  tracks: 'recent',
  albums: 'az',
  artists: 'az',
};

export function LibraryScreen(): ReactElement {
  const router = useRouter();
  const theme = useTheme();
  const state = useLibraryHome();
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

  const trackMenuItems = (track: TrackResponse) =>
    buildTrackMenuItems(track, {
      queue,
      onViewDetails: () => navigateToTrack(track),
      onAddToPlaylist: () => pl.setAddToPlaylistTrack(track),
      danger: { label: 'Remove from Library', onPress: () => deleteMutation.mutate(track.id) },
    });

  const sortKey = sortByChip[chip];
  const setSort = (key: SortKey): void => setSortByChip((prev) => ({ ...prev, [chip]: key }));

  const view = _viewForState({ isLoading: state.isLoading, error: state.error, items: state.allTracks });

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
            Check your connection and try again.
          </Text>
          <Button testID="library-retry" label="Retry" onPress={state.refetch} />
        </View>
      </Screen>
    );
  }

  if (view === 'empty' && pl.playlists.length === 0) {
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

  const retryingTrackId = retryMutation.isPending ? retryMutation.variables : undefined;
  const active = buildActiveView();

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
        trackTitle={pl.addToPlaylistTrack != null ? `${pl.addToPlaylistTrack.title} — ${pl.addToPlaylistTrack.artist}` : ''}
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

  function buildActiveView(): {
    content: ReactElement;
    count: number;
    noun: string;
    options: { key: SortKey; label: string }[];
  } {
    switch (chip) {
      case 'playlists': {
        const items = sortPlaylists(pl.playlists.filter((p) => search.matches(p.name)), sortKey);
        return {
          count: items.length,
          noun: 'playlist',
          options: PLAYLIST_SORT_OPTIONS,
          content: (
            <PlaylistsGrid
              playlists={items}
              onPlaylistPress={(playlist) => router.push(`/library/playlist/${playlist.id}`)}
              onCreatePress={() => pl.setCreateModalVisible(true)}
            />
          ),
        };
      }
      case 'tracks': {
        const items = sortTracks([...search.filter(state.allTracks)], sortKey);
        return {
          count: items.length,
          noun: 'track',
          options: TRACK_SORT_OPTIONS,
          content: (
            <TracksList
              tracks={items}
              emptyLabel={'No tracks yet'}
              onPlay={(track) => {
                const { playable, startIndex } = buildPlayableQueue(items, track.id);
                queue.playFromList(playable, startIndex, { kind: 'library' });
              }}
              onPress={navigateToTrack}
              onMore={(track, anchor) => setAction({ track, anchor })}
              onRetry={(track) => retryMutation.mutate(track.id)}
              retryingTrackId={retryingTrackId}
              isPlaying={(id) => isCurrentlyPlaying(playback, { kind: 'library', trackId: id })}
            />
          ),
        };
      }
      case 'albums': {
        const items = sortAlbums(
          state.albums.filter((a) => search.matches(a.album) || search.matches(a.artist)),
          sortKey,
        );
        return {
          count: items.length,
          noun: 'album',
          options: ALBUM_SORT_OPTIONS,
          content: (
            <AlbumsGrid
              albums={items}
              emptyLabel={'No albums yet'}
              onAlbumPress={navigateToAlbum}
            />
          ),
        };
      }
      case 'artists': {
        const items = sortArtists(state.artists.filter((a) => search.matches(a.artist)), sortKey);
        return {
          count: items.length,
          noun: 'artist',
          options: ARTIST_SORT_OPTIONS,
          content: (
            <ArtistsGrid
              artists={items}
              emptyLabel={'No artists yet'}
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

import type { ReactElement } from 'react';
import { Alert, RefreshControl, ScrollView, StyleSheet } from 'react-native';

import type { PlaylistResponse, TrackResponse } from '@shared/api-client/types';
import { Screen, spacing, useTheme } from '@shared/ui';

import type { AlbumGroup, ArtistGroup } from '../hooks/useLibraryGrouping';
import { AddToPlaylistSheet } from './AddToPlaylistSheet';
import { AlbumCarousel } from './AlbumCarousel';
import { ArtistCarousel } from './ArtistCarousel';
import { CreatePlaylistModal } from './CreatePlaylistModal';
import { LibraryHeader } from './LibraryHeader';
import { LibraryRow } from './LibraryRow';
import { PlaylistCarousel } from './PlaylistCarousel';
import { SectionHeader } from './SectionHeader';

export type PlaylistActions = {
  createModalVisible: boolean;
  onCreateModalToggle: (visible: boolean) => void;
  onCreatePlaylist: (name: string) => void;
  createLoading: boolean;
  addToPlaylistTrack: TrackResponse | null;
  onAddToPlaylistTrackChange: (track: TrackResponse | null) => void;
};

export type TrackActions = {
  onDeleteTrack: (trackId: string) => void;
  onRetryTrack: (trackId: string) => void;
  retryingTrackId?: string | undefined;
};

type LibraryHomeProps = {
  playlists: PlaylistResponse[];
  recentTracks: TrackResponse[];
  albums: AlbumGroup[];
  artists: ArtistGroup[];
  navigateToTrack: (track: TrackResponse) => void;
  navigateToAlbum: (album: AlbumGroup) => void;
  navigateToArtist: (artist: ArtistGroup) => void;
  onExpandRecent: () => void;
  onExpandAlbums: () => void;
  onExpandArtists: () => void;
  onPlaylistPress: (pl: PlaylistResponse) => void;
  onRefresh: () => void;
  playlistActions: PlaylistActions;
  trackActions: TrackActions;
};

export function LibraryHome({
  playlists,
  recentTracks,
  albums,
  artists,
  navigateToTrack,
  navigateToAlbum,
  navigateToArtist,
  onExpandRecent,
  onExpandAlbums,
  onExpandArtists,
  onPlaylistPress,
  onRefresh,
  playlistActions,
  trackActions,
}: LibraryHomeProps): ReactElement {
  const theme = useTheme();
  return (
    <Screen>
      <LibraryHeader />
      <ScrollView
        showsVerticalScrollIndicator={false}
        contentContainerStyle={styles.scroll}
        refreshControl={
          <RefreshControl
            refreshing={false}
            onRefresh={onRefresh}
            tintColor={theme.color.accent}
            colors={[theme.color.accent]}
          />
        }
      >
        <SectionHeader title="Playlists" />
        <PlaylistCarousel
          playlists={playlists}
          onPlaylistPress={onPlaylistPress}
          onCreatePress={() => playlistActions.onCreateModalToggle(true)}
        />

        {recentTracks.length > 0 ? (
          <>
            <SectionHeader
              title="Recently Added"
              onSeeAll={onExpandRecent}
              testID="library-section-recent"
            />
            {recentTracks.map((track) => (
              <LibraryRow
                key={track.id}
                track={track}
                onPress={() => navigateToTrack(track)}
                onLongPress={() => {
                  Alert.alert(track.title, undefined, [
                    { text: 'Add to Playlist', onPress: () => playlistActions.onAddToPlaylistTrackChange(track) },
                    { text: 'Remove from Library', style: 'destructive', onPress: () => trackActions.onDeleteTrack(track.id) },
                    { text: 'Cancel', style: 'cancel' },
                  ]);
                }}
                onRetry={
                  track.acquisition_status === 'failed'
                    ? () => trackActions.onRetryTrack(track.id)
                    : undefined
                }
                retrying={trackActions.retryingTrackId === track.id}
              />
            ))}
          </>
        ) : null}

        {albums.length > 0 ? (
          <>
            <SectionHeader
              title="Albums"
              onSeeAll={onExpandAlbums}
              testID="library-section-albums"
            />
            <AlbumCarousel albums={albums} onAlbumPress={navigateToAlbum} />
          </>
        ) : null}

        {artists.length > 0 ? (
          <>
            <SectionHeader
              title="Artists"
              onSeeAll={onExpandArtists}
              testID="library-section-artists"
            />
            <ArtistCarousel artists={artists} onArtistPress={navigateToArtist} />
          </>
        ) : null}
      </ScrollView>
      <CreatePlaylistModal
        visible={playlistActions.createModalVisible}
        onClose={() => playlistActions.onCreateModalToggle(false)}
        onCreate={playlistActions.onCreatePlaylist}
        loading={playlistActions.createLoading}
      />
      <AddToPlaylistSheet
        visible={playlistActions.addToPlaylistTrack != null}
        trackId={playlistActions.addToPlaylistTrack?.id ?? ''}
        trackTitle={playlistActions.addToPlaylistTrack != null ? `${playlistActions.addToPlaylistTrack.title} — ${playlistActions.addToPlaylistTrack.artist}` : ''}
        onClose={() => playlistActions.onAddToPlaylistTrackChange(null)}
      />
    </Screen>
  );
}

const styles = StyleSheet.create({
  scroll: { paddingBottom: spacing['3xl'] },
});

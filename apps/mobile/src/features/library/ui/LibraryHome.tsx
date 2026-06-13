import type { ReactElement } from 'react';
import { RefreshControl, ScrollView, StyleSheet } from 'react-native';

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
  createModalVisible: boolean;
  onCreateModalToggle: (visible: boolean) => void;
  onCreatePlaylist: (name: string) => void;
  createLoading: boolean;
  addToPlaylistTrack: TrackResponse | null;
  onAddToPlaylistTrackChange: (track: TrackResponse | null) => void;
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
  createModalVisible,
  onCreateModalToggle,
  onCreatePlaylist,
  createLoading,
  addToPlaylistTrack,
  onAddToPlaylistTrackChange,
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
          onCreatePress={() => onCreateModalToggle(true)}
        />

        {recentTracks.length > 0 ? (
          <>
            <SectionHeader
              title="Recently Added"
              onSeeAll={onExpandRecent}
              testID="library-section-recent"
            />
            {recentTracks.map((track) => (
              <LibraryRow key={track.id} track={track} onPress={() => navigateToTrack(track)} onLongPress={() => onAddToPlaylistTrackChange(track)} />
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
        visible={createModalVisible}
        onClose={() => onCreateModalToggle(false)}
        onCreate={onCreatePlaylist}
        loading={createLoading}
      />
      <AddToPlaylistSheet
        visible={addToPlaylistTrack != null}
        trackId={addToPlaylistTrack?.id ?? ''}
        trackTitle={addToPlaylistTrack != null ? `${addToPlaylistTrack.title} — ${addToPlaylistTrack.artist}` : ''}
        onClose={() => onAddToPlaylistTrackChange(null)}
      />
    </Screen>
  );
}

const styles = StyleSheet.create({
  scroll: { paddingBottom: spacing['3xl'] },
});

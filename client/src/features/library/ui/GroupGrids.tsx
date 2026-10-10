import { memo, useCallback, type ReactElement } from 'react';
import type { ListRenderItem } from 'react-native';
import { Pressable, StyleSheet, View } from 'react-native';
import { Image as ExpoImage } from 'expo-image';
import { Text, pressedStyle, radius, spacing, useTheme } from '@shared/ui';
import type { AlbumGroup, ArtistGroup } from '@shared/apiClient/library';
import { LibraryGrid } from '@features/library/ui/LibraryGrid';
import { useLibraryGridLayout, GRID_GAP } from '@features/library/hooks/useLibraryGridLayout';
import type { ListPaging, ListRefresh } from '@features/library/domain/libraryView';

type AlbumsGridProps = {
  albums: AlbumGroup[];
  emptyLabel: string;
  refresh: ListRefresh;
  onAlbumPress: (album: AlbumGroup) => void;
  paging?: ListPaging;
};

type AlbumTileProps = { album: AlbumGroup; onPress: (album: AlbumGroup) => void };

type PressState = { pressed: boolean };

function albumTileStyle({ pressed }: PressState) {
  return [styles.gridItem, pressedStyle(pressed)];
}

type AlbumCoverProps = { uri: AlbumGroup['artwork_url'] };

function AlbumCover({ uri }: AlbumCoverProps): ReactElement {
  const theme = useTheme();
  const background = { backgroundColor: theme.color.surface2 };
  return (
    <View style={[styles.cover, background]}>
      {uri != null ? <ExpoImage source={{ uri }} {...coverImageProps} /> : null}
    </View>
  );
}

type AlbumCaptionProps = { album: AlbumGroup };

function AlbumCaption({ album }: AlbumCaptionProps): ReactElement {
  const year = album.year != null ? ` · ${album.year}` : '';
  return (
    <>
      <Text {...albumTitleProps}>{album.album}</Text>
      <Text {...albumSubtitleProps}>{`${album.artist}${year}`}</Text>
    </>
  );
}

function albumTileProps(album: AlbumGroup, onPress: () => void) {
  return {
    testID: `library-album-${album.key}`,
    accessibilityRole: 'button',
    accessibilityLabel: `${album.album} by ${album.artist}`,
    onPress,
  } as const;
}

function AlbumTileImpl({ album, onPress }: AlbumTileProps): ReactElement {
  const press = useCallback(() => onPress(album), [onPress, album]);
  return (
    <Pressable {...albumTileProps(album, press)} style={albumTileStyle}>
      <AlbumCover uri={album.artwork_url} />
      <AlbumCaption album={album} />
    </Pressable>
  );
}

const AlbumTile = memo(AlbumTileImpl);

export function AlbumsGrid({
  albums,
  emptyLabel,
  refresh,
  onAlbumPress,
  paging,
}: AlbumsGridProps): ReactElement {
  const { columns, onLayout } = useLibraryGridLayout('cover');
  const renderItem = useCallback<ListRenderItem<AlbumGroup>>(
    ({ item }) => <AlbumTile album={item} onPress={onAlbumPress} />,
    [onAlbumPress],
  );
  return (
    <LibraryGrid
      testID="library-albums-grid"
      data={albums}
      keyExtractor={(a) => a.key}
      columns={columns}
      onLayout={onLayout}
      refresh={refresh}
      emptyLabel={emptyLabel}
      paging={paging}
      renderItem={renderItem}
    />
  );
}

const styles = StyleSheet.create({
  gridItem: { flex: 1, marginBottom: spacing.lg },
  cover: {
    width: '100%',
    aspectRatio: 1,
    borderRadius: radius.sm,
    overflow: 'hidden',
  },
  coverImage: { width: '100%', height: '100%' },
});

const coverImageProps = { style: styles.coverImage, contentFit: 'cover' } as const;
const albumTitleProps = { variant: 'label', numberOfLines: 1 } as const;
const albumSubtitleProps = { variant: 'caption', tone: 'secondary', numberOfLines: 1 } as const;

type ArtistsGridProps = {
  artists: ArtistGroup[];
  emptyLabel: string;
  refresh: ListRefresh;
  onArtistPress: (artist: ArtistGroup) => void;
  paging?: ListPaging;
};

const AVATAR_SIZE = 100;

type ArtistTileProps = { artist: ArtistGroup; onPress: (artist: ArtistGroup) => void };

type ArtistAvatarProps = { uri: ArtistGroup['artwork_url'] };

function ArtistAvatar({ uri }: ArtistAvatarProps): ReactElement {
  const theme = useTheme();
  const background = { backgroundColor: theme.color.surface2 };
  return (
    <View style={[artistStyles.avatar, background]}>
      {uri != null ? <ExpoImage source={{ uri }} {...avatarImageProps} /> : null}
    </View>
  );
}

function artistTileProps(artist: ArtistGroup, onPress: () => void) {
  return {
    testID: `library-artist-${artist.key}`,
    style: artistStyles.gridItem,
    accessibilityRole: 'button',
    accessibilityLabel: artist.artist,
    onPress,
  } as const;
}

function ArtistTileImpl({ artist, onPress }: ArtistTileProps): ReactElement {
  const press = useCallback(() => onPress(artist), [onPress, artist]);
  return (
    <Pressable {...artistTileProps(artist, press)}>
      <ArtistAvatar uri={artist.artwork_url} />
      <Text {...artistNameProps}>{artist.artist}</Text>
    </Pressable>
  );
}

const ArtistTile = memo(ArtistTileImpl);

export function ArtistsGrid({
  artists,
  emptyLabel,
  refresh,
  onArtistPress,
  paging,
}: ArtistsGridProps): ReactElement {
  const { columns, onLayout } = useLibraryGridLayout('avatar');
  const renderItem = useCallback<ListRenderItem<ArtistGroup>>(
    ({ item }) => <ArtistTile artist={item} onPress={onArtistPress} />,
    [onArtistPress],
  );
  return (
    <LibraryGrid
      testID="library-artists-grid"
      data={artists}
      keyExtractor={(a) => a.key}
      columns={columns}
      onLayout={onLayout}
      columnWrapperStyle={artistStyles.gridRow}
      refresh={refresh}
      emptyLabel={emptyLabel}
      paging={paging}
      renderItem={renderItem}
    />
  );
}

const artistStyles = StyleSheet.create({
  gridRow: { gap: GRID_GAP, justifyContent: 'flex-start' },
  gridItem: { alignItems: 'center', marginBottom: spacing.lg, width: AVATAR_SIZE + spacing.sm },
  avatar: {
    width: AVATAR_SIZE,
    height: AVATAR_SIZE,
    borderRadius: radius.full,
    overflow: 'hidden',
  },
  avatarImage: { width: AVATAR_SIZE, height: AVATAR_SIZE },
  name: { textAlign: 'center', marginTop: spacing.xs, width: AVATAR_SIZE },
});

const avatarImageProps = { style: artistStyles.avatarImage, contentFit: 'cover' } as const;
const artistNameProps = {
  variant: 'caption',
  numberOfLines: 1,
  style: artistStyles.name,
} as const;

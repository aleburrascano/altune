import type { ReactElement } from 'react';
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

export function AlbumsGrid({
  albums,
  emptyLabel,
  refresh,
  onAlbumPress,
  paging,
}: AlbumsGridProps): ReactElement {
  const theme = useTheme();
  const { columns, onLayout } = useLibraryGridLayout('cover');
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
      renderItem={({ item }) => (
        <Pressable
          testID={`library-album-${item.key}`}
          style={({ pressed }) => [styles.gridItem, pressedStyle(pressed)]}
          onPress={() => onAlbumPress(item)}
          accessibilityRole="button"
          accessibilityLabel={`${item.album} by ${item.artist}`}
        >
          <View style={[styles.cover, { backgroundColor: theme.color.surface2 }]}>
            {item.artwork_url != null ? (
              <ExpoImage
                source={{ uri: item.artwork_url }}
                style={styles.coverImage}
                contentFit="cover"
              />
            ) : null}
          </View>
          <Text variant="label" numberOfLines={1}>
            {item.album}
          </Text>
          <Text variant="caption" tone="secondary" numberOfLines={1}>
            {item.artist}
            {item.year != null ? ` · ${item.year}` : ''}
          </Text>
        </Pressable>
      )}
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

type ArtistsGridProps = {
  artists: ArtistGroup[];
  emptyLabel: string;
  refresh: ListRefresh;
  onArtistPress: (artist: ArtistGroup) => void;
  paging?: ListPaging;
};

const AVATAR_SIZE = 100;

export function ArtistsGrid({
  artists,
  emptyLabel,
  refresh,
  onArtistPress,
  paging,
}: ArtistsGridProps): ReactElement {
  const theme = useTheme();
  const { columns, onLayout } = useLibraryGridLayout('avatar');
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
      renderItem={({ item }) => (
        <Pressable
          testID={`library-artist-${item.key}`}
          style={artistStyles.gridItem}
          onPress={() => onArtistPress(item)}
          accessibilityRole="button"
          accessibilityLabel={item.artist}
        >
          <View style={[artistStyles.avatar, { backgroundColor: theme.color.surface2 }]}>
            {item.artwork_url != null ? (
              <ExpoImage
                source={{ uri: item.artwork_url }}
                style={artistStyles.avatarImage}
                contentFit="cover"
              />
            ) : null}
          </View>
          <Text variant="caption" numberOfLines={1} style={artistStyles.name}>
            {item.artist}
          </Text>
        </Pressable>
      )}
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

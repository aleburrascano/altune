import type { ReactElement } from 'react';
import { Pressable, StyleSheet, View } from 'react-native';
import { Image as ExpoImage } from 'expo-image';

import { Text, radius, spacing, useTheme } from '@shared/ui';

import type { AlbumGroup } from '@shared/api-client/library';
import { LibraryGrid } from './LibraryGrid';
import { useLibraryGridLayout } from './useLibraryGridLayout';
import type { ListPaging, ListRefresh } from '../refresh';

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
          style={({ pressed }) => [styles.gridItem, pressed ? styles.pressed : null]}
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
  pressed: { opacity: 0.7 },
  cover: {
    width: '100%',
    aspectRatio: 1,
    borderRadius: radius.sm,
    overflow: 'hidden',
  },
  coverImage: { width: '100%', height: '100%' },
});

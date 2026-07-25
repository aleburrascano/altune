import type { ReactElement } from 'react';
import { FlatList, Pressable, StyleSheet, useWindowDimensions, View } from 'react-native';
import { Image as ExpoImage } from 'expo-image';

import { Text, radius, spacing, useTheme } from '@shared/ui';

import type { AlbumGroup } from '@shared/api-client/library';
import { coverColumns } from './gridColumns';
import type { ListRefresh } from './refresh';

type AlbumsGridProps = {
  albums: AlbumGroup[];
  emptyLabel: string;
  refresh: ListRefresh;
  onAlbumPress: (album: AlbumGroup) => void;
};

export function AlbumsGrid({
  albums,
  emptyLabel,
  refresh,
  onAlbumPress,
}: AlbumsGridProps): ReactElement {
  const theme = useTheme();
  const { width } = useWindowDimensions();
  const columns = coverColumns(width);
  return (
    <FlatList
      testID="library-albums-grid"
      data={albums}
      keyExtractor={(a) => a.key}
      key={`cols-${columns}`}
      numColumns={columns}
      columnWrapperStyle={styles.gridRow}
      contentContainerStyle={albums.length === 0 ? styles.emptyList : styles.list}
      showsVerticalScrollIndicator={false}
      onRefresh={refresh.onRefresh}
      refreshing={refresh.refreshing}
      ListEmptyComponent={
        <View style={styles.empty}>
          <Text variant="body" tone="secondary">
            {emptyLabel}
          </Text>
        </View>
      }
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
  list: { paddingBottom: spacing['3xl'] },
  emptyList: { flexGrow: 1 },
  gridRow: { gap: spacing.md },
  gridItem: { flex: 1, marginBottom: spacing.lg },
  pressed: { opacity: 0.7 },
  cover: {
    width: '100%',
    aspectRatio: 1,
    borderRadius: radius.sm,
    overflow: 'hidden',
  },
  coverImage: { width: '100%', height: '100%' },
  empty: { flex: 1, alignItems: 'center', paddingTop: spacing['3xl'] },
});

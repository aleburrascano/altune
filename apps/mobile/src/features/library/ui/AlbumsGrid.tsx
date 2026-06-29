import type { ReactElement } from 'react';
import { FlatList, Pressable, RefreshControl, StyleSheet, View } from 'react-native';
import { Image as ExpoImage } from 'expo-image';

import { Text, radius, spacing, useTheme } from '@shared/ui';

import type { AlbumGroup } from '../hooks/useLibraryGrouping';

type AlbumsGridProps = {
  albums: AlbumGroup[];
  emptyLabel: string;
  onAlbumPress: (album: AlbumGroup) => void;
  onRefresh: () => void;
};

export function AlbumsGrid({ albums, emptyLabel, onAlbumPress, onRefresh }: AlbumsGridProps): ReactElement {
  const theme = useTheme();
  return (
    <FlatList
      testID="library-albums-grid"
      data={albums}
      keyExtractor={(a) => a.key}
      numColumns={2}
      columnWrapperStyle={styles.gridRow}
      contentContainerStyle={albums.length === 0 ? styles.emptyList : styles.list}
      showsVerticalScrollIndicator={false}
      refreshControl={
        <RefreshControl refreshing={false} onRefresh={onRefresh} tintColor={theme.color.accent} colors={[theme.color.accent]} />
      }
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
            {item.artworkUrl != null ? (
              <ExpoImage source={{ uri: item.artworkUrl }} style={styles.coverImage} contentFit="cover" />
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

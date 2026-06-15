import { useState, type ReactElement } from 'react';
import { FlatList, Pressable, StyleSheet, View } from 'react-native';
import { useRouter } from 'expo-router';
import { Image as ExpoImage } from 'expo-image';

import { Screen, Text, radius, spacing, useTheme } from '@shared/ui';

import type { AlbumGroup } from '../hooks/useLibraryGrouping';
import { useLibraryHome } from '../hooks/useLibraryHome';
import { useLibraryNavigation } from './useLibraryNavigation';
import { ExpandedHeader } from './ExpandedHeader';
import { sortAlbums, ALBUM_SORT_OPTIONS } from './sort';
import type { SortKey } from './sort';

export function AllAlbumsScreen(): ReactElement {
  const router = useRouter();
  const theme = useTheme();
  const state = useLibraryHome();
  const { navigateToAlbum } = useLibraryNavigation(router);
  const [sortKey, setSortKey] = useState<SortKey>('recent');

  const sorted = sortAlbums(state.albums, sortKey);

  return (
    <Screen>
      <ExpandedHeader
        title="Albums"
        onBack={() => router.back()}
        sortKey={sortKey}
        onSortChange={setSortKey}
        sortOptions={ALBUM_SORT_OPTIONS}
      />
      <FlatList
        data={sorted}
        keyExtractor={(a) => a.key}
        numColumns={2}
        columnWrapperStyle={styles.gridRow}
        contentContainerStyle={styles.list}
        showsVerticalScrollIndicator={false}
        renderItem={({ item }: { item: AlbumGroup }) => (
          <Pressable
            style={({ pressed }) => [styles.gridItem, pressed ? styles.pressed : null]}
            onPress={() => navigateToAlbum(item)}
            accessibilityRole="button"
            accessibilityLabel={`${item.album} by ${item.artist}`}
          >
            <View style={{ width: '100%', aspectRatio: 1 }}>
              <View
                style={[styles.cover, { backgroundColor: theme.color.surface2 }]}
              >
                {item.artworkUrl != null ? (
                  <ExpoImage source={{ uri: item.artworkUrl }} style={styles.coverImage} contentFit="cover" />
                ) : null}
              </View>
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
    </Screen>
  );
}

const styles = StyleSheet.create({
  list: { paddingBottom: spacing['3xl'] },
  gridRow: { gap: spacing.md },
  gridItem: { flex: 1, marginBottom: spacing.lg },
  pressed: { opacity: 0.7 },
  cover: {
    width: '100%',
    height: '100%',
    borderRadius: radius.sm,
    overflow: 'hidden',
  },
  coverImage: { width: '100%', height: '100%' },
});

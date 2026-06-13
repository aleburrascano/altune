import { useCallback, type ReactElement } from 'react';
import { FlatList, type ListRenderItemInfo, Pressable, StyleSheet, View } from 'react-native';

import { Artwork, Text, spacing } from '@shared/ui';

import type { AlbumGroup } from '../hooks/useLibraryGrouping';

type AlbumCarouselProps = {
  albums: AlbumGroup[];
  onAlbumPress: (album: AlbumGroup) => void;
};

const COVER_SIZE = 110;

export function AlbumCarousel({ albums, onAlbumPress }: AlbumCarouselProps): ReactElement {
  const renderItem = useCallback(
    ({ item }: ListRenderItemInfo<AlbumGroup>) => (
      <Pressable
        testID={`library-album-${item.key}`}
        onPress={() => onAlbumPress(item)}
        style={({ pressed }) => [styles.card, pressed ? styles.pressed : null]}
        accessibilityRole="button"
        accessibilityLabel={`${item.album} by ${item.artist}`}
      >
        <Artwork uri={item.artworkUrl} size={COVER_SIZE} radius={8} />
        <View style={styles.meta}>
          <Text variant="label" numberOfLines={1}>
            {item.album}
          </Text>
          <Text variant="caption" tone="secondary" numberOfLines={1}>
            {item.artist}
          </Text>
        </View>
      </Pressable>
    ),
    [onAlbumPress],
  );

  return (
    <FlatList
      testID="library-album-carousel"
      data={albums}
      keyExtractor={(item) => item.key}
      horizontal
      showsHorizontalScrollIndicator={false}
      contentContainerStyle={styles.list}
      renderItem={renderItem}
    />
  );
}

const styles = StyleSheet.create({
  list: { paddingHorizontal: spacing.lg, gap: spacing.md },
  card: { width: COVER_SIZE },
  pressed: { opacity: 0.7 },
  meta: { marginTop: spacing.xs },
});

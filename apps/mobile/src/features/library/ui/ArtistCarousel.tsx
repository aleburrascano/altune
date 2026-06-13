import { useCallback, type ReactElement } from 'react';
import { FlatList, type ListRenderItemInfo, Pressable, StyleSheet, View } from 'react-native';

import { Artwork, Text, radius as radiusTokens, spacing } from '@shared/ui';

import type { ArtistGroup } from '../hooks/useLibraryGrouping';

type ArtistCarouselProps = {
  artists: ArtistGroup[];
  onArtistPress: (artist: ArtistGroup) => void;
};

const CIRCLE_SIZE = 72;

export function ArtistCarousel({ artists, onArtistPress }: ArtistCarouselProps): ReactElement {
  const renderItem = useCallback(
    ({ item }: ListRenderItemInfo<ArtistGroup>) => (
      <Pressable
        testID={`library-artist-${item.key}`}
        onPress={() => onArtistPress(item)}
        style={({ pressed }) => [styles.card, pressed ? styles.pressed : null]}
        accessibilityRole="button"
        accessibilityLabel={item.artist}
      >
        <Artwork
          uri={item.artworkUrl}
          size={CIRCLE_SIZE}
          radius={radiusTokens.full}
          accessibilityLabel={`${item.artist} artwork`}
        />
        <View style={styles.meta}>
          <Text variant="caption" numberOfLines={1} style={styles.name}>
            {item.artist}
          </Text>
        </View>
      </Pressable>
    ),
    [onArtistPress],
  );

  return (
    <FlatList
      testID="library-artist-carousel"
      data={artists}
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
  card: { width: CIRCLE_SIZE, alignItems: 'center' },
  pressed: { opacity: 0.7 },
  meta: { marginTop: spacing.xs, alignItems: 'center', width: CIRCLE_SIZE },
  name: { textAlign: 'center' },
});

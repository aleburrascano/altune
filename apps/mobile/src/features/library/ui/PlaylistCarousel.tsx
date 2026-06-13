import { useCallback, type ReactElement } from 'react';
import { FlatList, type ListRenderItemInfo, Pressable, StyleSheet, View } from 'react-native';

import { Text, spacing, useTheme } from '@shared/ui';
import type { PlaylistResponse } from '@shared/api-client/types';

import { PlaylistCover } from './PlaylistCover';

type PlaylistCarouselProps = {
  playlists: PlaylistResponse[];
  onPlaylistPress: (playlist: PlaylistResponse) => void;
  onCreatePress: () => void;
};

const COVER_SIZE = 110;

export function PlaylistCarousel({
  playlists,
  onPlaylistPress,
  onCreatePress,
}: PlaylistCarouselProps): ReactElement {
  const theme = useTheme();

  const renderItem = useCallback(
    ({ item }: ListRenderItemInfo<PlaylistResponse>) => (
      <Pressable
        testID={`library-playlist-${item.id}`}
        onPress={() => onPlaylistPress(item)}
        style={({ pressed }) => [styles.card, pressed ? styles.pressed : null]}
        accessibilityRole="button"
        accessibilityLabel={`${item.name}, ${item.track_count} tracks`}
      >
        <PlaylistCover artworkUrls={item.preview_artwork_urls} size={COVER_SIZE} />
        <View style={styles.meta}>
          <Text variant="label" numberOfLines={1}>
            {item.name}
          </Text>
          <Text variant="caption" tone="secondary" numberOfLines={1}>
            {item.track_count} {item.track_count === 1 ? 'track' : 'tracks'}
          </Text>
        </View>
      </Pressable>
    ),
    [onPlaylistPress],
  );

  return (
    <FlatList
      testID="library-playlist-carousel"
      data={playlists}
      keyExtractor={(item) => item.id}
      horizontal
      showsHorizontalScrollIndicator={false}
      contentContainerStyle={styles.list}
      ListHeaderComponent={
        <Pressable
          testID="library-create-playlist"
          onPress={onCreatePress}
          style={({ pressed }) => [styles.createCard, pressed ? styles.pressed : null]}
          accessibilityRole="button"
          accessibilityLabel="Create new playlist"
        >
          <View
            style={[
              styles.createCover,
              { backgroundColor: theme.color.surface2, borderColor: theme.color.border },
            ]}
          >
            <Text variant="displayL" tone="tertiary">
              +
            </Text>
          </View>
          <Text variant="label" tone="secondary" numberOfLines={1}>
            Create
          </Text>
        </Pressable>
      }
      renderItem={renderItem}
    />
  );
}

const styles = StyleSheet.create({
  list: { paddingHorizontal: spacing.lg, gap: spacing.md },
  card: { width: COVER_SIZE },
  createCard: { width: COVER_SIZE, marginRight: spacing.md },
  createCover: {
    width: COVER_SIZE,
    height: COVER_SIZE,
    borderRadius: 8,
    borderWidth: 1,
    borderStyle: 'dashed',
    alignItems: 'center',
    justifyContent: 'center',
  },
  pressed: { opacity: 0.7 },
  meta: { marginTop: spacing.xs },
});

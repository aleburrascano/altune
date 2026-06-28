import type { ReactElement } from 'react';
import { FlatList, Pressable, RefreshControl, StyleSheet, View } from 'react-native';
import { Image as ExpoImage } from 'expo-image';

import { Text, radius, spacing, useTheme } from '@shared/ui';

import type { ArtistGroup } from '../hooks/useLibraryGrouping';

type ArtistsGridProps = {
  artists: ArtistGroup[];
  emptyLabel: string;
  onArtistPress: (artist: ArtistGroup) => void;
  onRefresh: () => void;
};

const AVATAR_SIZE = 100;

export function ArtistsGrid({ artists, emptyLabel, onArtistPress, onRefresh }: ArtistsGridProps): ReactElement {
  const theme = useTheme();
  return (
    <FlatList
      testID="library-artists-grid"
      data={artists}
      keyExtractor={(a) => a.key}
      numColumns={3}
      columnWrapperStyle={styles.gridRow}
      contentContainerStyle={artists.length === 0 ? styles.emptyList : styles.list}
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
          testID={`library-artist-${item.key}`}
          style={styles.gridItem}
          onPress={() => onArtistPress(item)}
          accessibilityRole="button"
          accessibilityLabel={item.artist}
        >
          <View style={[styles.avatar, { backgroundColor: theme.color.surface2 }]}>
            {item.artworkUrl != null ? (
              <ExpoImage source={{ uri: item.artworkUrl }} style={styles.avatarImage} contentFit="cover" />
            ) : null}
          </View>
          <Text variant="caption" numberOfLines={1} style={styles.name}>
            {item.artist}
          </Text>
        </Pressable>
      )}
    />
  );
}

const styles = StyleSheet.create({
  list: { paddingBottom: spacing['3xl'] },
  emptyList: { flexGrow: 1 },
  gridRow: { gap: spacing.md, justifyContent: 'flex-start' },
  gridItem: { alignItems: 'center', marginBottom: spacing.lg, width: AVATAR_SIZE + spacing.sm },
  avatar: {
    width: AVATAR_SIZE,
    height: AVATAR_SIZE,
    borderRadius: radius.full,
    overflow: 'hidden',
  },
  avatarImage: { width: AVATAR_SIZE, height: AVATAR_SIZE },
  name: { textAlign: 'center', marginTop: spacing.xs, width: AVATAR_SIZE },
  empty: { flex: 1, alignItems: 'center', paddingTop: spacing['3xl'] },
});

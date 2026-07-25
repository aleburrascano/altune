import type { ReactElement } from 'react';
import { FlatList, Pressable, StyleSheet, useWindowDimensions, View } from 'react-native';
import { Image as ExpoImage } from 'expo-image';

import { Text, radius, spacing, useTheme } from '@shared/ui';

import type { ArtistGroup } from '@shared/api-client/library';
import { avatarColumns } from './gridColumns';
import type { ListRefresh } from './refresh';

type ArtistsGridProps = {
  artists: ArtistGroup[];
  emptyLabel: string;
  refresh: ListRefresh;
  onArtistPress: (artist: ArtistGroup) => void;
};

const AVATAR_SIZE = 100;

export function ArtistsGrid({
  artists,
  emptyLabel,
  refresh,
  onArtistPress,
}: ArtistsGridProps): ReactElement {
  const theme = useTheme();
  const { width } = useWindowDimensions();
  const columns = avatarColumns(width);
  return (
    <FlatList
      testID="library-artists-grid"
      data={artists}
      keyExtractor={(a) => a.key}
      key={`cols-${columns}`}
      numColumns={columns}
      columnWrapperStyle={styles.gridRow}
      contentContainerStyle={artists.length === 0 ? styles.emptyList : styles.list}
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
          testID={`library-artist-${item.key}`}
          style={styles.gridItem}
          onPress={() => onArtistPress(item)}
          accessibilityRole="button"
          accessibilityLabel={item.artist}
        >
          <View style={[styles.avatar, { backgroundColor: theme.color.surface2 }]}>
            {item.artwork_url != null ? (
              <ExpoImage
                source={{ uri: item.artwork_url }}
                style={styles.avatarImage}
                contentFit="cover"
              />
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

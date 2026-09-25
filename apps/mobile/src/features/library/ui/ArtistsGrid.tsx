import type { ReactElement } from 'react';
import { Pressable, StyleSheet, View } from 'react-native';
import { Image as ExpoImage } from 'expo-image';

import { Text, radius, spacing, useTheme } from '@shared/ui';

import type { ArtistGroup } from '@shared/api-client/library';
import { GRID_GAP } from '../gridColumns';
import { LibraryGrid } from './LibraryGrid';
import { useLibraryGridLayout } from './useLibraryGridLayout';
import type { ListRefresh } from '../refresh';

type ArtistsGridProps = {
  artists: ArtistGroup[];
  emptyLabel: string;
  refresh: ListRefresh;
  onArtistPress: (artist: ArtistGroup) => void;
  onEndReached?: () => void;
  isFetchingNextPage?: boolean;
  nextPageFailed?: boolean;
  onRetryNextPage?: () => void;
};

const AVATAR_SIZE = 100;

export function ArtistsGrid({
  artists,
  emptyLabel,
  refresh,
  onArtistPress,
  onEndReached,
  isFetchingNextPage,
  nextPageFailed,
  onRetryNextPage,
}: ArtistsGridProps): ReactElement {
  const theme = useTheme();
  const { columns } = useLibraryGridLayout('avatar');
  return (
    <LibraryGrid
      testID="library-artists-grid"
      data={artists}
      keyExtractor={(a) => a.key}
      columns={columns}
      columnWrapperStyle={styles.gridRow}
      refresh={refresh}
      emptyLabel={emptyLabel}
      onEndReached={onEndReached}
      isFetchingNextPage={isFetchingNextPage}
      nextPageFailed={nextPageFailed}
      onRetryNextPage={onRetryNextPage}
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

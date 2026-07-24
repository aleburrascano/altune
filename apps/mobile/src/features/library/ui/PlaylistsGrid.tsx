import { useCallback, type ReactElement } from 'react';
import { FlatList, Pressable, StyleSheet, useWindowDimensions, View } from 'react-native';

import type { PlaylistResponse } from '@shared/api-client/types';
import { Text, radius, spacing, useTheme } from '@shared/ui';

import { cellSize, coverColumns } from './gridColumns';
import { PlaylistCover } from './PlaylistCover';
import type { ListRefresh } from './refresh';

type Cell = { kind: 'create' } | { kind: 'playlist'; playlist: PlaylistResponse };

type PlaylistsGridProps = {
  playlists: PlaylistResponse[];
  refresh: ListRefresh;
  onPlaylistPress: (playlist: PlaylistResponse) => void;
  onCreatePress: () => void;
};

export function PlaylistsGrid({
  playlists,
  refresh,
  onPlaylistPress,
  onCreatePress,
}: PlaylistsGridProps): ReactElement {
  const theme = useTheme();
  const { width } = useWindowDimensions();
  const columns = coverColumns(width);
  const coverSize = cellSize({
    width,
    columns,
    horizontalPadding: spacing.lg,
    gap: spacing.md,
  });

  const data: Cell[] = [
    { kind: 'create' },
    ...playlists.map((playlist) => ({ kind: 'playlist' as const, playlist })),
  ];

  const renderItem = useCallback(
    ({ item }: { item: Cell }) => {
      if (item.kind === 'create') {
        return (
          <Pressable
            testID="library-create-playlist"
            onPress={onCreatePress}
            style={({ pressed }) => [styles.cell, pressed ? styles.pressed : null]}
            accessibilityRole="button"
            accessibilityLabel="Create new playlist"
          >
            <View
              style={[
                styles.createCover,
                {
                  width: coverSize,
                  height: coverSize,
                  backgroundColor: theme.color.surface2,
                  borderColor: theme.color.border,
                },
              ]}
            >
              <Text variant="displayL" tone="tertiary">
                +
              </Text>
            </View>
            <Text variant="label" tone="secondary" numberOfLines={1}>
              New Playlist
            </Text>
          </Pressable>
        );
      }
      const { playlist } = item;
      return (
        <Pressable
          testID={`library-playlist-${playlist.id}`}
          onPress={() => onPlaylistPress(playlist)}
          style={({ pressed }) => [styles.cell, pressed ? styles.pressed : null]}
          accessibilityRole="button"
          accessibilityLabel={`${playlist.name}, ${playlist.track_count} tracks`}
        >
          <PlaylistCover artworkUrls={playlist.preview_artwork_urls} size={coverSize} />
          <Text variant="label" numberOfLines={1} style={styles.name}>
            {playlist.name}
          </Text>
          <Text variant="caption" tone="secondary" numberOfLines={1}>
            {playlist.track_count} {playlist.track_count === 1 ? 'track' : 'tracks'}
          </Text>
        </Pressable>
      );
    },
    [coverSize, onCreatePress, onPlaylistPress, theme.color.border, theme.color.surface2],
  );

  return (
    <FlatList
      testID="library-playlists-grid"
      data={data}
      keyExtractor={(item) => (item.kind === 'create' ? 'create' : item.playlist.id)}
      key={`cols-${columns}`}
      numColumns={columns}
      columnWrapperStyle={styles.gridRow}
      contentContainerStyle={styles.list}
      showsVerticalScrollIndicator={false}
      onRefresh={refresh.onRefresh}
      refreshing={refresh.refreshing}
      renderItem={renderItem}
    />
  );
}

const styles = StyleSheet.create({
  list: { paddingBottom: spacing['3xl'] },
  gridRow: { gap: spacing.md },
  cell: { flex: 1, marginBottom: spacing.lg },
  pressed: { opacity: 0.7 },
  name: { marginTop: spacing.xs },
  createCover: {
    borderRadius: radius.sm,
    borderWidth: 1,
    borderStyle: 'dashed',
    alignItems: 'center',
    justifyContent: 'center',
  },
});

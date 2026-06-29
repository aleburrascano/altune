import { useCallback, type ReactElement } from 'react';
import {
  FlatList,
  Pressable,
  RefreshControl,
  StyleSheet,
  useWindowDimensions,
  View,
} from 'react-native';

import type { PlaylistResponse } from '@shared/api-client/types';
import { Text, radius, spacing, useTheme } from '@shared/ui';

import { PlaylistCover } from './PlaylistCover';

type Cell = { kind: 'create' } | { kind: 'playlist'; playlist: PlaylistResponse };

type PlaylistsGridProps = {
  playlists: PlaylistResponse[];
  onPlaylistPress: (playlist: PlaylistResponse) => void;
  onCreatePress: () => void;
  onRefresh: () => void;
};

export function PlaylistsGrid({
  playlists,
  onPlaylistPress,
  onCreatePress,
  onRefresh,
}: PlaylistsGridProps): ReactElement {
  const theme = useTheme();
  const { width } = useWindowDimensions();
  const coverSize = Math.floor((width - spacing.lg * 2 - spacing.md) / 2);

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
                { width: coverSize, height: coverSize, backgroundColor: theme.color.surface2, borderColor: theme.color.border },
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
      numColumns={2}
      columnWrapperStyle={styles.gridRow}
      contentContainerStyle={styles.list}
      showsVerticalScrollIndicator={false}
      renderItem={renderItem}
      refreshControl={
        <RefreshControl refreshing={false} onRefresh={onRefresh} tintColor={theme.color.accent} colors={[theme.color.accent]} />
      }
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

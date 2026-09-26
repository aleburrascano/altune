import { useCallback, type ReactElement } from 'react';
import { Pressable, StyleSheet, View } from 'react-native';

import type { PlaylistResponse } from '@shared/api-client/types';
import { countLabel } from '@shared/lib/format';
import { Text, radius, spacing, useTheme } from '@shared/ui';

import { LibraryGrid } from './LibraryGrid';
import { PlaylistCover } from './PlaylistCover';
import { useLibraryGridLayout } from './useLibraryGridLayout';
import type { ListPaging, ListRefresh } from '../refresh';

type Cell = { kind: 'create' } | { kind: 'playlist'; playlist: PlaylistResponse };

type PlaylistsGridProps = {
  playlists: PlaylistResponse[];
  refresh: ListRefresh;
  onPlaylistPress: (playlist: PlaylistResponse) => void;
  onCreatePress: () => void;
  paging?: ListPaging;
};

export function PlaylistsGrid({
  playlists,
  refresh,
  onPlaylistPress,
  onCreatePress,
  paging,
}: PlaylistsGridProps): ReactElement {
  const theme = useTheme();
  const { columns, cellSize: coverSize, onLayout } = useLibraryGridLayout('cover');

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
            {playlist.track_count} {countLabel(playlist.track_count, 'track')}
          </Text>
        </Pressable>
      );
    },
    [coverSize, onCreatePress, onPlaylistPress, theme.color.border, theme.color.surface2],
  );

  return (
    <LibraryGrid
      testID="library-playlists-grid"
      data={data}
      keyExtractor={(item) => (item.kind === 'create' ? 'create' : item.playlist.id)}
      columns={columns}
      onLayout={onLayout}
      refresh={refresh}
      paging={paging}
      renderItem={renderItem}
    />
  );
}

const styles = StyleSheet.create({
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

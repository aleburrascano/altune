import type { ReactElement } from 'react';
import { FlatList, Pressable, StyleSheet, View } from 'react-native';

import { Screen, Text, radius, spacing, useTheme } from '@shared/ui';

import type { AlbumGroup } from '../hooks/useLibraryGrouping';
import { ExpandedHeader } from './ExpandedHeader';
import { LibraryHeader } from './LibraryHeader';
import { ProfileSheet } from './ProfileSheet';
import { sortAlbums, ALBUM_SORT_OPTIONS } from './sort';
import type { SortKey } from './sort';

// AIDEV-NOTE: Lazy import to avoid pulling expo-image into non-expanded renders
const { Image: ExpoImage } = require('expo-image') as { Image: typeof import('expo-image').Image };

type ExpandedAlbumsProps = {
  albums: AlbumGroup[];
  sortKey: SortKey;
  onSortChange: (key: SortKey) => void;
  onCollapse: () => void;
  navigateToAlbum: (album: AlbumGroup) => void;
  initial: string;
  email: string;
  profileVisible: boolean;
  onProfileToggle: (visible: boolean) => void;
};

export function ExpandedAlbums({
  albums,
  sortKey,
  onSortChange,
  onCollapse,
  navigateToAlbum,
  initial,
  email,
  profileVisible,
  onProfileToggle,
}: ExpandedAlbumsProps): ReactElement {
  const theme = useTheme();
  const sorted = sortAlbums(albums, sortKey);
  return (
    <Screen>
      <LibraryHeader initial={initial} onAvatarPress={() => onProfileToggle(true)} />
      <ExpandedHeader
        title="Albums"
        onCollapse={onCollapse}
        sortKey={sortKey}
        onSortChange={onSortChange}
        sortOptions={ALBUM_SORT_OPTIONS}
      />
      <FlatList
        data={sorted}
        keyExtractor={(a) => a.key}
        numColumns={2}
        columnWrapperStyle={styles.albumGridRow}
        contentContainerStyle={styles.expandedList}
        showsVerticalScrollIndicator={false}
        renderItem={({ item }) => (
          <Pressable
            style={styles.albumGridItem}
            onPress={() => navigateToAlbum(item)}
            accessibilityRole="button"
            accessibilityLabel={`${item.album} by ${item.artist}`}
          >
            <View style={styles.albumGridCover}>
              <View style={{ width: '100%', aspectRatio: 1 }}>
                <View
                  style={{
                    width: '100%',
                    height: '100%',
                    borderRadius: radius.sm,
                    backgroundColor: theme.color.surface2,
                    overflow: 'hidden',
                  }}
                >
                  {item.artworkUrl != null ? (
                    <ExpoImage source={{ uri: item.artworkUrl }} style={{ width: '100%', height: '100%' }} contentFit="cover" />
                  ) : null}
                </View>
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
      <ProfileSheet visible={profileVisible} email={email} onClose={() => onProfileToggle(false)} />
    </Screen>
  );
}

const styles = StyleSheet.create({
  expandedList: { paddingBottom: spacing['3xl'] },
  albumGridRow: { gap: spacing.md, paddingHorizontal: spacing.lg },
  albumGridItem: { flex: 1, marginBottom: spacing.lg },
  albumGridCover: { marginBottom: spacing.xs },
});

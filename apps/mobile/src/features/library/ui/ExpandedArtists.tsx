import type { ReactElement } from 'react';
import { FlatList, Pressable, StyleSheet, View } from 'react-native';

import { Screen, Text, radius, spacing, useTheme } from '@shared/ui';

import type { ArtistGroup } from '../hooks/useLibraryGrouping';
import { ExpandedHeader } from './ExpandedHeader';
import { LibraryHeader } from './LibraryHeader';
import { ProfileSheet } from './ProfileSheet';
import { sortArtists, ARTIST_SORT_OPTIONS } from './sort';
import type { SortKey } from './sort';

// AIDEV-NOTE: Lazy import to avoid pulling expo-image into non-expanded renders
const { Image: ExpoImage } = require('expo-image') as { Image: typeof import('expo-image').Image };

type ExpandedArtistsProps = {
  artists: ArtistGroup[];
  sortKey: SortKey;
  onSortChange: (key: SortKey) => void;
  onCollapse: () => void;
  navigateToArtist: (artist: ArtistGroup) => void;
  initial: string;
  email: string;
  profileVisible: boolean;
  onProfileToggle: (visible: boolean) => void;
};

export function ExpandedArtists({
  artists,
  sortKey,
  onSortChange,
  onCollapse,
  navigateToArtist,
  initial,
  email,
  profileVisible,
  onProfileToggle,
}: ExpandedArtistsProps): ReactElement {
  const theme = useTheme();
  const sorted = sortArtists(artists, sortKey);
  return (
    <Screen>
      <LibraryHeader initial={initial} onAvatarPress={() => onProfileToggle(true)} />
      <ExpandedHeader
        title="Artists"
        onCollapse={onCollapse}
        sortKey={sortKey}
        onSortChange={onSortChange}
        sortOptions={ARTIST_SORT_OPTIONS}
      />
      <FlatList
        data={sorted}
        keyExtractor={(a) => a.key}
        numColumns={3}
        columnWrapperStyle={styles.artistGridRow}
        contentContainerStyle={styles.expandedList}
        showsVerticalScrollIndicator={false}
        renderItem={({ item }) => (
          <Pressable
            style={styles.artistGridItem}
            onPress={() => navigateToArtist(item)}
            accessibilityRole="button"
            accessibilityLabel={item.artist}
          >
            <View
              style={{
                width: 80,
                height: 80,
                borderRadius: radius.full,
                backgroundColor: theme.color.surface2,
                overflow: 'hidden',
              }}
            >
              {item.artworkUrl != null ? (
                <ExpoImage source={{ uri: item.artworkUrl }} style={{ width: 80, height: 80 }} contentFit="cover" />
              ) : null}
            </View>
            <Text variant="caption" numberOfLines={1} style={styles.artistGridName}>
              {item.artist}
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
  artistGridRow: { gap: spacing.md, paddingHorizontal: spacing.lg, justifyContent: 'flex-start' },
  artistGridItem: { alignItems: 'center', marginBottom: spacing.lg, width: 88 },
  artistGridName: { textAlign: 'center', marginTop: spacing.xs, width: 80 },
});

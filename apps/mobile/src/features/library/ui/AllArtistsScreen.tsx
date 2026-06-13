import { useState, type ReactElement } from 'react';
import { FlatList, Pressable, StyleSheet, View } from 'react-native';
import { useRouter } from 'expo-router';
import { Image as ExpoImage } from 'expo-image';

import { Screen, Text, radius, spacing, useTheme } from '@shared/ui';

import type { ArtistGroup } from '../hooks/useLibraryGrouping';
import { useLibraryHome } from '../hooks/useLibraryHome';
import { useLibraryNavigation } from './useLibraryNavigation';
import { ExpandedHeader } from './ExpandedHeader';
import { sortArtists, ARTIST_SORT_OPTIONS } from './sort';
import type { SortKey } from './sort';

export function AllArtistsScreen(): ReactElement {
  const router = useRouter();
  const theme = useTheme();
  const state = useLibraryHome();
  const { navigateToArtist } = useLibraryNavigation(router);
  const [sortKey, setSortKey] = useState<SortKey>('recent');

  const sorted = sortArtists(state.artists, sortKey);

  return (
    <Screen>
      <ExpandedHeader
        title="Artists"
        onBack={() => router.back()}
        sortKey={sortKey}
        onSortChange={setSortKey}
        sortOptions={ARTIST_SORT_OPTIONS}
      />
      <FlatList
        data={sorted}
        keyExtractor={(a) => a.key}
        numColumns={3}
        columnWrapperStyle={styles.gridRow}
        contentContainerStyle={styles.list}
        showsVerticalScrollIndicator={false}
        renderItem={({ item }: { item: ArtistGroup }) => (
          <Pressable
            style={styles.gridItem}
            onPress={() => navigateToArtist(item)}
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
    </Screen>
  );
}

const AVATAR_SIZE = 100;

const styles = StyleSheet.create({
  list: { paddingBottom: spacing['3xl'] },
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
});

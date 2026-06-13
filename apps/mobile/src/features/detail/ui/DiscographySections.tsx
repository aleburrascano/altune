import type { ReactElement } from 'react';
import { Pressable, ScrollView, StyleSheet, View } from 'react-native';

import { Artwork } from '@shared/ui/primitives/Artwork';
import { Text } from '@shared/ui/primitives/Text';
import { radius, spacing } from '@shared/ui/theme/tokens';

import type { DiscoveryResult } from '@shared/api-client/discovery';

import { _albumYear, sharedStyles } from './helpers';

const SECTION_CAP = 10;

const DISCOGRAPHY_SECTIONS: ReadonlyArray<{ type: string; label: string }> = [
  { type: 'album', label: 'Albums' },
  { type: 'single', label: 'Singles' },
  { type: 'ep', label: 'EPs' },
];

export function DiscographySections({
  albums,
  onAlbumPress,
}: {
  albums: DiscoveryResult[];
  onAlbumPress: (album: DiscoveryResult) => void;
}): ReactElement {
  const grouped = new Map<string, DiscoveryResult[]>();
  for (const album of albums) {
    const rawType = album.extras['record_type'];
    const type = typeof rawType === 'string' ? rawType.toLowerCase() : 'album';
    const bucket = type === 'compilation' ? 'album' : type;
    const list = grouped.get(bucket);
    if (list) {
      list.push(album);
    } else {
      grouped.set(bucket, [album]);
    }
  }

  return (
    <>
      {DISCOGRAPHY_SECTIONS.map((section) => {
        const items = grouped.get(section.type);
        if (!items || items.length === 0) return null;
        const capped = items.slice(0, SECTION_CAP);
        const hasMore = items.length > SECTION_CAP;
        return (
          <View key={section.type} style={sharedStyles.albumsSection}>
            <Text variant="label" tone="secondary" style={sharedStyles.sectionTitle}>
              {section.label} ({items.length})
            </Text>
            <ScrollView
              horizontal
              showsHorizontalScrollIndicator={false}
              style={styles.albumsScroll}
              contentContainerStyle={styles.albumsScrollContent}
            >
              {capped.map((album, index) => {
                const year = _albumYear(album);
                const trackCount = typeof album.extras['track_count'] === 'number'
                  ? album.extras['track_count']
                  : null;
                return (
                  <Pressable
                    key={album.sources[0]?.external_id ?? index}
                    testID={`detail-${section.type}-${index}`}
                    onPress={() => onAlbumPress(album)}
                    accessibilityRole="button"
                    accessibilityLabel={`${section.label}: ${album.title}${year ? `, ${year}` : ''}${trackCount ? `, ${trackCount} tracks` : ''}`}
                    style={({ pressed }) => [styles.albumCard, pressed ? { opacity: 0.6 } : null]}
                  >
                    <Artwork
                      uri={album.image_url}
                      size={120}
                      radius={radius.md}
                      accessibilityLabel={album.title}
                    />
                    <Text variant="label" numberOfLines={2} style={styles.albumTitle}>
                      {album.title}
                    </Text>
                    {year ? (
                      <Text variant="caption" tone="tertiary">
                        {year}
                      </Text>
                    ) : null}
                    {trackCount !== null ? (
                      <Text variant="caption" tone="tertiary">
                        {trackCount} tracks
                      </Text>
                    ) : null}
                  </Pressable>
                );
              })}
              {hasMore ? (
                <View style={styles.seeAllCard}>
                  <Text variant="label" tone="accent" style={styles.seeAllText}>
                    See all {items.length} {section.label.toLowerCase()}
                  </Text>
                </View>
              ) : null}
            </ScrollView>
          </View>
        );
      })}
    </>
  );
}

const styles = StyleSheet.create({
  albumsScroll: { marginHorizontal: -spacing.lg },
  albumsScrollContent: { paddingRight: spacing.lg },
  albumCard: { width: 120, marginLeft: spacing.lg },
  albumTitle: { marginTop: spacing.xs },
  seeAllCard: {
    width: 120,
    height: 120,
    marginLeft: spacing.lg,
    alignItems: 'center',
    justifyContent: 'center',
  },
  seeAllText: { textAlign: 'center' },
});

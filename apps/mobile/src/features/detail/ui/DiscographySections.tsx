import { useState, type ReactElement } from 'react';
import { Pressable, ScrollView, StyleSheet, View } from 'react-native';

import { ChevronRight } from 'lucide-react-native';

import { Artwork } from '@shared/ui/primitives/Artwork';
import { Text } from '@shared/ui/primitives/Text';
import { useTheme } from '@shared/ui/theme';
import { radius, spacing } from '@shared/ui/theme/tokens';

import type { DiscoveryResult } from '@shared/api-client/discovery';

import { albumExtras } from '../extras-accessors';
import { _albumYear, sharedStyles } from './helpers';

const SECTION_CAP = 10;

const DISCOGRAPHY_SECTIONS: readonly { type: string; label: string }[] = [
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
  const theme = useTheme();
  const [expandedSections, setExpandedSections] = useState<Set<string>>(new Set());
  const grouped = new Map<string, DiscoveryResult[]>();
  for (const album of albums) {
    const type = albumExtras(album.extras).recordType?.toLowerCase() ?? 'album';
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
        const isExpanded = expandedSections.has(section.type);
        const capped = isExpanded ? items : items.slice(0, SECTION_CAP);
        const hasMore = !isExpanded && items.length > SECTION_CAP;
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
                const trackCount = albumExtras(album.extras).trackCount;
                return (
                  <Pressable
                    key={`${album.title}-${album.sources[0]?.external_id ?? index}`}
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
                <Pressable
                  testID={`detail-see-all-${section.type}`}
                  style={({ pressed }) => [styles.seeAllCard, { backgroundColor: theme.color.surface2 }, pressed ? { opacity: 0.6 } : null]}
                  onPress={() => setExpandedSections((prev) => new Set(prev).add(section.type))}
                  accessibilityRole="button"
                  accessibilityLabel={`See all ${items.length} ${section.label.toLowerCase()}`}
                >
                  <ChevronRight size={20} color={theme.color.accent} />
                  <Text variant="label" tone="accent" style={styles.seeAllText}>
                    See all
                  </Text>
                </Pressable>
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
    borderRadius: radius.md,
    gap: spacing.xs,
  },
  seeAllText: { textAlign: 'center' },
});

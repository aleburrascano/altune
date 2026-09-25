import { useState, type ReactElement } from 'react';
import { FlatList, Pressable, StyleSheet, View } from 'react-native';

import { ChevronRight } from 'lucide-react-native';

import { Artwork } from '@shared/ui/primitives/Artwork';
import { Text } from '@shared/ui/primitives/Text';
import { radius, spacing, useTheme } from '@shared/ui/theme';

import type { DiscoveryResult } from '@shared/api-client/discovery';

import { albumExtras } from '../extras-accessors';
import { albumYear } from './formatters';
import { DETAIL_GUTTER, DISCOGRAPHY_CARD_SIZE } from './layout';
import { sharedStyles } from './styles';

const SECTION_CAP = 10;

const RECORD_TYPES: readonly { type: string; label: string }[] = [
  { type: 'album', label: 'Albums' },
  { type: 'single', label: 'Singles' },
  { type: 'ep', label: 'EPs' },
];

function groupByRecordType(albums: DiscoveryResult[]): Map<string, DiscoveryResult[]> {
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
  return grouped;
}

export function DiscographySections({
  albums,
  onAlbumPress,
}: {
  albums: DiscoveryResult[];
  onAlbumPress: (album: DiscoveryResult) => void;
}): ReactElement | null {
  const theme = useTheme();
  const [expanded, setExpanded] = useState(false);
  const [selected, setSelected] = useState<string | null>(null);

  const grouped = groupByRecordType(albums);
  const present = RECORD_TYPES.filter((t) => (grouped.get(t.type)?.length ?? 0) > 0);

  if (present.length === 0) {
    return null;
  }

  const active = present.find((t) => t.type === selected) ?? present[0]!;
  const items = grouped.get(active.type) ?? [];
  const capped = expanded ? items : items.slice(0, SECTION_CAP);
  const hasMore = !expanded && items.length > SECTION_CAP;

  return (
    <View>
      {present.length > 1 ? (
        <View style={styles.chips}>
          {present.map((t) => {
            const count = grouped.get(t.type)?.length ?? 0;
            const on = t.type === active.type;
            return (
              <Pressable
                key={t.type}
                testID={`detail-discography-${t.type}`}
                onPress={() => {
                  setSelected(t.type);
                  setExpanded(false);
                }}
                accessibilityRole="button"
                accessibilityLabel={`${t.label}, ${count}`}
                accessibilityState={{ selected: on }}
                style={({ pressed }) => [
                  styles.chip,
                  {
                    backgroundColor: on ? theme.color.accent : theme.color.surface2,
                    borderColor: on ? 'transparent' : theme.color.border,
                  },
                  pressed ? sharedStyles.pressed : null,
                ]}
              >
                <Text variant="label" tone={on ? 'onAccent' : 'secondary'}>
                  {t.label} {count}
                </Text>
              </Pressable>
            );
          })}
        </View>
      ) : null}

      <FlatList
        testID="detail-discography-rail"
        horizontal
        showsHorizontalScrollIndicator={false}
        style={styles.rail}
        contentContainerStyle={styles.railContent}
        data={capped}
        // Expanding a long discography must cost no more cards than the collapsed
        // rail already showed; the rest mount as they are scrolled to (#1668).
        initialNumToRender={SECTION_CAP}
        keyExtractor={(album, index) => `${album.title}-${album.sources[0]?.external_id ?? index}`}
        renderItem={({ item, index }) => (
          <AlbumCard
            album={item}
            testID={`detail-${active.type}-${index}`}
            typeLabel={active.label}
            onPress={() => onAlbumPress(item)}
          />
        )}
        ListFooterComponent={
          hasMore ? (
            <Pressable
              testID={`detail-see-all-${active.type}`}
              onPress={() => setExpanded(true)}
              accessibilityRole="button"
              accessibilityLabel={`See all ${items.length} ${active.label.toLowerCase()}`}
              style={({ pressed }) => [
                styles.seeAll,
                { backgroundColor: theme.color.surface2 },
                pressed ? sharedStyles.pressed : null,
              ]}
            >
              <ChevronRight size={20} color={theme.color.accent} />
              <Text variant="label" tone="accent" style={styles.seeAllText}>
                See all
              </Text>
            </Pressable>
          ) : null
        }
      />
    </View>
  );
}

// One rail card. Extracted from DiscographySections so the card's own optional
// lines (year, track count) live here rather than inside the list's renderItem.
function AlbumCard({
  album,
  testID,
  typeLabel,
  onPress,
}: {
  album: DiscoveryResult;
  testID: string;
  typeLabel: string;
  onPress: () => void;
}): ReactElement {
  const year = albumYear(album);
  const trackCount = albumExtras(album.extras).trackCount;
  return (
    <Pressable
      testID={testID}
      onPress={onPress}
      accessibilityRole="button"
      accessibilityLabel={`${typeLabel}: ${album.title}${year ? `, ${year}` : ''}${trackCount !== null ? `, ${trackCount} tracks` : ''}`}
      style={({ pressed }) => [styles.card, pressed ? sharedStyles.pressed : null]}
    >
      <Artwork
        uri={album.image_url}
        size={DISCOGRAPHY_CARD_SIZE}
        radius={radius.md}
        accessibilityLabel={album.title}
      />
      <Text variant="label" numberOfLines={2} style={styles.cardTitle}>
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
}

const styles = StyleSheet.create({
  chips: { flexDirection: 'row', gap: spacing.sm, marginBottom: spacing.md, flexWrap: 'wrap' },
  chip: {
    minHeight: 36,
    paddingHorizontal: spacing.md,
    justifyContent: 'center',
    borderRadius: radius.full,
    borderWidth: StyleSheet.hairlineWidth,
  },
  rail: { marginHorizontal: -DETAIL_GUTTER },
  railContent: { paddingHorizontal: DETAIL_GUTTER, gap: spacing.md },
  card: { width: DISCOGRAPHY_CARD_SIZE },
  cardTitle: { marginTop: spacing.xs },
  seeAll: {
    width: DISCOGRAPHY_CARD_SIZE,
    height: DISCOGRAPHY_CARD_SIZE,
    alignItems: 'center',
    justifyContent: 'center',
    borderRadius: radius.md,
    gap: spacing.xs,
  },
  seeAllText: { textAlign: 'center' },
});

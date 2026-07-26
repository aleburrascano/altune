import { useState, type ReactElement } from 'react';
import { Pressable, ScrollView, StyleSheet, View } from 'react-native';

import { ChevronRight } from 'lucide-react-native';

import { Artwork } from '@shared/ui/primitives/Artwork';
import { Text } from '@shared/ui/primitives/Text';
import { radius, spacing, useTheme } from '@shared/ui/theme';

import type { DiscoveryResult } from '@shared/api-client/discovery';

import { albumExtras } from '../extras-accessors';
import { _albumYear } from './helpers';

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
                  pressed ? styles.pressed : null,
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

      <ScrollView
        horizontal
        showsHorizontalScrollIndicator={false}
        style={styles.rail}
        contentContainerStyle={styles.railContent}
      >
        {capped.map((album, index) => {
          const year = _albumYear(album);
          const trackCount = albumExtras(album.extras).trackCount;
          return (
            <Pressable
              key={`${album.title}-${album.sources[0]?.external_id ?? index}`}
              testID={`detail-${active.type}-${index}`}
              onPress={() => onAlbumPress(album)}
              accessibilityRole="button"
              accessibilityLabel={`${active.label}: ${album.title}${year ? `, ${year}` : ''}${trackCount ? `, ${trackCount} tracks` : ''}`}
              style={({ pressed }) => [styles.card, pressed ? styles.pressed : null]}
            >
              <Artwork
                uri={album.image_url}
                size={128}
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
        })}
        {hasMore ? (
          <Pressable
            testID={`detail-see-all-${active.type}`}
            onPress={() => setExpanded(true)}
            accessibilityRole="button"
            accessibilityLabel={`See all ${items.length} ${active.label.toLowerCase()}`}
            style={({ pressed }) => [
              styles.seeAll,
              { backgroundColor: theme.color.surface2 },
              pressed ? styles.pressed : null,
            ]}
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
  rail: { marginHorizontal: -spacing.lg },
  railContent: { paddingHorizontal: spacing.lg, gap: spacing.md },
  card: { width: 128 },
  cardTitle: { marginTop: spacing.xs },
  seeAll: {
    width: 128,
    height: 128,
    alignItems: 'center',
    justifyContent: 'center',
    borderRadius: radius.md,
    gap: spacing.xs,
  },
  seeAllText: { textAlign: 'center' },
  pressed: { opacity: 0.6 },
});

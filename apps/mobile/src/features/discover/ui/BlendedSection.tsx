import type { ReactElement } from 'react';
import { FlatList, Pressable, StyleSheet, View } from 'react-native';

import { Text, spacing } from '@shared/ui';

import { DiscoverRow } from './DiscoverRow';
import { TopResultCard } from './TopResultCard';
import {
  SECTION_CAP,
  _cap,
  _groupByKind,
  _sectionOrder,
  _topResult,
} from '../state';

import type { SectionKey } from '../state';
import type {
  DiscoveryKind,
  DiscoveryResult,
} from '../../../shared/api-client/discovery';

export function BlendedSection({
  results,
  onResultTap,
  onSeeAll,
}: {
  results: DiscoveryResult[];
  onResultTap: (result: DiscoveryResult, position: number) => void;
  onSeeAll: (filter: DiscoveryKind) => void;
}): ReactElement {
  const top = _topResult(results);
  const { albums, songs, artists } = _groupByKind(results);

  const byKind: Record<
    DiscoveryKind,
    { title: string; sectionKey: SectionKey; items: DiscoveryResult[] }
  > = {
    album: { title: 'Albums', sectionKey: 'album', items: albums },
    track: { title: 'Songs', sectionKey: 'song', items: songs },
    artist: { title: 'Artists', sectionKey: 'artist', items: artists },
  };
  // Order containers by which kind best matches the query (the kind whose
  // strongest member ranks earliest), so a song query shows Songs first.
  const order = _sectionOrder(results);
  const sections = (['album', 'track', 'artist'] as const)
    .map((kind) => ({ kind, ...byKind[kind] }))
    .filter((s) => s.items.length > 0)
    .sort((a, b) => order.indexOf(a.sectionKey) - order.indexOf(b.sectionKey));

  return (
    <FlatList
      data={sections}
      keyExtractor={(s) => s.kind}
      ListHeaderComponent={
        top !== null ? <TopResultCard result={top} onPress={onResultTap} /> : null
      }
      renderItem={({ item: section }) => (
        <View style={styles.section}>
          <Text variant="label" tone="tertiary" style={styles.sectionHeader}>
            {section.title.toUpperCase()}
          </Text>
          {_cap(section.items).map((result, index) => (
            <DiscoverRow
              key={`${result.kind}-${result.sources[0]?.provider ?? 'x'}-${result.sources[0]?.external_id || `${result.title}-${index}`}`}
              result={result}
              position={index}
              onPress={onResultTap}
            />
          ))}
          {section.items.length > SECTION_CAP ? (
            <Pressable
              testID={`discover-see-all-${section.kind}`}
              onPress={() => onSeeAll(section.kind)}
              accessibilityRole="button"
              accessibilityLabel={`See all ${section.title.toLowerCase()}`}
              style={({ pressed }) => [styles.seeAll, pressed ? { opacity: 0.7 } : null]}
            >
              <Text variant="label" tone="accent">
                See all {section.title.toLowerCase()}
              </Text>
            </Pressable>
          ) : null}
        </View>
      )}
      contentContainerStyle={styles.listContent}
      showsVerticalScrollIndicator={false}
    />
  );
}

const styles = StyleSheet.create({
  listContent: { paddingTop: spacing.sm, paddingBottom: spacing.xl },
  sectionHeader: { marginBottom: spacing.md, letterSpacing: 1 },
  section: { marginBottom: spacing.lg },
  seeAll: { paddingVertical: spacing.md, alignSelf: 'flex-start', minHeight: 44 },
});

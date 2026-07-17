import type { ReactElement } from 'react';
import { Pressable, StyleSheet, View } from 'react-native';
import { ChevronRight } from 'lucide-react-native';

import { Text, spacing, useTheme } from '@shared/ui';

import { DiscoverRow } from './DiscoverRow';
import { ResultsList, type ResultsCommonProps } from './ResultsList';
import { TopResultCard } from './TopResultCard';
import {
  SECTION_CAP,
  _cap,
  _groupByKind,
  _sectionOrder,
  _topResult,
  kindLabel,
  resultKey,
} from '../state';

import type { SectionKey } from '../state';
import type {
  DiscoveryKind,
  DiscoveryResult,
} from '@shared/api-client/discovery';

export function BlendedSection({
  results,
  onSeeAll,
  common,
}: {
  results: DiscoveryResult[];
  onSeeAll: (filter: DiscoveryKind) => void;
  common: ResultsCommonProps;
}): ReactElement {
  const theme = useTheme();
  const top = _topResult(results);
  const { albums, tracks, artists } = _groupByKind(results);

  const byKind: Record<
    DiscoveryKind,
    { title: string; sectionKey: SectionKey; items: DiscoveryResult[] }
  > = {
    album: { title: kindLabel('album', { plural: true }), sectionKey: 'album', items: albums },
    track: { title: kindLabel('track', { plural: true }), sectionKey: 'track', items: tracks },
    artist: { title: kindLabel('artist', { plural: true }), sectionKey: 'artist', items: artists },
  };
  if (top !== null) {
    const kindList = byKind[top.kind];
    kindList.items = kindList.items.filter((r) => r !== top);
  }
  const order = _sectionOrder(results);
  const sections = (['album', 'track', 'artist'] as const)
    .map((kind) => ({ kind, ...byKind[kind] }))
    .filter((s) => s.items.length > 0)
    .sort((a, b) => order.indexOf(a.sectionKey) - order.indexOf(b.sectionKey));

  return (
    <ResultsList
      data={sections}
      keyExtractor={(s) => s.kind}
      headerExtra={top !== null ? <TopResultCard result={top} onPress={common.onResultTap} /> : null}
      common={common}
      renderItem={({ item: section }) => (
        <View style={styles.section}>
          <Text variant="label" tone="tertiary" style={styles.sectionHeader}>
            {section.title.toUpperCase()}
          </Text>
          {_cap(section.items).map((result, index) => (
            <DiscoverRow
              key={resultKey(result, index)}
              result={result}
              position={index}
              onPress={common.onResultTap}
            />
          ))}
          {section.items.length > SECTION_CAP ? (
            <Pressable
              testID={`discover-see-all-${section.kind}`}
              onPress={() => onSeeAll(section.kind)}
              accessibilityRole="button"
              accessibilityLabel={`See all ${section.title.toLowerCase()}`}
              hitSlop={8}
              style={({ pressed }) => [styles.seeAll, pressed ? { opacity: 0.7 } : null]}
            >
              <Text variant="label" tone="accent">
                See all {section.title.toLowerCase()}
              </Text>
              <ChevronRight size={16} color={theme.color.accent} />
            </Pressable>
          ) : null}
        </View>
      )}
    />
  );
}

const styles = StyleSheet.create({
  sectionHeader: { marginBottom: spacing.sm, marginTop: spacing.sm, letterSpacing: 1 },
  section: { marginBottom: spacing.xl },
  seeAll: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.xs,
    paddingVertical: spacing.md,
    alignSelf: 'flex-start',
    minHeight: 44,
  },
});

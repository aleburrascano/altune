import type { ReactElement } from 'react';
import { FlatList, Pressable, StyleSheet, View } from 'react-native';
import { ChevronRight } from 'lucide-react-native';

import { Text, spacing, useTheme } from '@shared/ui';

import { CorrectionBanner } from './CorrectionBanner';
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
  onRefresh,
  isRefreshing,
  correctedQuery,
  originalQuery,
  onSearchOriginal,
}: {
  results: DiscoveryResult[];
  onResultTap: (result: DiscoveryResult, position: number) => void;
  onSeeAll: (filter: DiscoveryKind) => void;
  onRefresh: () => void;
  isRefreshing: boolean;
  correctedQuery?: string | undefined;
  originalQuery?: string | undefined;
  onSearchOriginal: () => void;
}): ReactElement {
  const theme = useTheme();
  const top = _topResult(results);
  const { albums, tracks, artists } = _groupByKind(results);

  const byKind: Record<
    DiscoveryKind,
    { title: string; sectionKey: SectionKey; items: DiscoveryResult[] }
  > = {
    album: { title: 'Albums', sectionKey: 'album', items: albums },
    track: { title: 'Songs', sectionKey: 'track', items: tracks },
    artist: { title: 'Artists', sectionKey: 'artist', items: artists },
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

  const header = (
    <>
      {correctedQuery && originalQuery ? (
        <CorrectionBanner
          correctedQuery={correctedQuery}
          originalQuery={originalQuery}
          onSearchOriginal={onSearchOriginal}
        />
      ) : null}
      {top !== null ? <TopResultCard result={top} onPress={onResultTap} /> : null}
    </>
  );

  return (
    <FlatList
      data={sections}
      keyExtractor={(s) => s.kind}
      ListHeaderComponent={header}
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
      style={styles.list}
      contentContainerStyle={styles.listContent}
      onRefresh={onRefresh}
      refreshing={isRefreshing}
    />
  );
}

const styles = StyleSheet.create({
  list: { flex: 1 },
  listContent: { paddingTop: spacing.sm, paddingBottom: spacing.xl, flexGrow: 1 },
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
